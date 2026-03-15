package xiaohongshu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"

	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

//nolint:unused
const (
	defaultMaxVideoSize    = 500 * 1024 * 1024 // 500MB
	defaultMaxAudioChunk   = 20 * 1024 * 1024  // 20MB (Groq API limit)
	groqWhisperAPIEndpoint = "https://api.groq.com/openai/v1/audio/transcriptions"
)

var validFeedIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// TranscribeVideoAction 视频转录操作
type TranscribeVideoAction struct {
	config *configs.TranscriptionConfig
	logger *logrus.Logger
}

// NewTranscribeVideoAction 创建转录操作实例
func NewTranscribeVideoAction(config *configs.TranscriptionConfig, logger *logrus.Logger) *TranscribeVideoAction {
	if config == nil {
		config = configs.DefaultTranscriptionConfig()
	}
	return &TranscribeVideoAction{
		config: config,
		logger: logger,
	}
}

// validateFeedID 验证 feedID 格式
func validateFeedID(feedID string) error {
	if !validFeedIDPattern.MatchString(feedID) {
		return fmt.Errorf("invalid feed_id: must contain only alphanumeric characters, hyphens and underscores")
	}
	return nil
}

// ExtractVideoInfo 从小红书页面提取视频信息
func (t *TranscribeVideoAction) ExtractVideoInfo(page *rod.Page, feedID string) (*VideoInfo, error) {
	if err := validateFeedID(feedID); err != nil {
		return nil, err
	}

	// 从 window.__INITIAL_STATE__ 提取笔记数据
	result, err := page.Eval(`(feedID) => {
		if (window.__INITIAL_STATE__ &&
			window.__INITIAL_STATE__.note &&
			window.__INITIAL_STATE__.note.noteDetailMap) {
			const note = window.__INITIAL_STATE__.note.noteDetailMap[feedID];
			if (note) {
				return JSON.stringify(note);
			}
		}
		return "";
	}`, feedID)

	if err != nil {
		return nil, fmt.Errorf("提取页面数据失败: %w", err)
	}

	evalResult := result.Value.String()

	if evalResult == "" {
		return nil, fmt.Errorf("无法获取笔记数据，请检查 feed_id 和登录状态")
	}

	// 解析 JSON 数据
	var noteDetail struct {
		Note struct {
			Type  string `json:"type"`
			Title string `json:"title"`
			Video *Video `json:"video"`
			User  struct {
				Nickname string `json:"nickname"`
			} `json:"user"`
		} `json:"note"`
	}

	if err := json.Unmarshal([]byte(evalResult), &noteDetail); err != nil {
		return nil, fmt.Errorf("解析笔记数据失败: %w", err)
	}

	// 验证是否为视频笔记
	if !IsVideoNote(noteDetail.Note.Type, noteDetail.Note.Video) {
		return nil, fmt.Errorf("该笔记不是视频笔记，无法转录")
	}

	// 提取视频 URL
	videoURL := t.extractVideoURL(page, feedID)
	if videoURL == "" {
		return nil, fmt.Errorf("无法获取视频下载地址")
	}

	return &VideoInfo{
		Title:    noteDetail.Note.Title,
		Author:   noteDetail.Note.User.Nickname,
		Duration: noteDetail.Note.Video.Capa.Duration,
		VideoURL: videoURL,
	}, nil
}

// extractVideoURL 提取视频 CDN 地址
func (t *TranscribeVideoAction) extractVideoURL(page *rod.Page, feedID string) string {
	// 尝试从页面数据中提取视频 URL
	result, err := page.Eval(`(feedID) => {
		const state = window.__INITIAL_STATE__;
		if (state && state.note && state.note.noteDetailMap) {
			const note = state.note.noteDetailMap[feedID];
			if (note && note.note && note.note.video) {
				const video = note.note.video;
				// 尝试不同的视频地址字段
				if (video.videoConsumer && video.videoConsumer.originVideoKey) {
					return video.videoConsumer.originVideoKey;
				}
				if (video.videoConsumer && video.videoConsumer.url) {
					return video.videoConsumer.url;
				}
				if (video.media && video.media.stream) {
					return video.media.stream.h264[0];
				}
			}
		}
		return "";
	}`, feedID)

	if err != nil {
		t.logger.WithError(err).Error("提取视频URL失败")
		return ""
	}

	return result.Value.String()
}

// DownloadVideo 下载视频到指定目录
func (t *TranscribeVideoAction) DownloadVideo(videoURL, feedID string, maxSize int, outputDir string) (string, error) {
	if maxSize == 0 {
		maxSize = defaultMaxVideoSize
	}

	// 如果未指定输出目录，使用系统临时目录
	if outputDir == "" {
		outputDir = os.TempDir()
	}

	videoPath := filepath.Join(outputDir, fmt.Sprintf("xhs_%s_%d.mp4", feedID, time.Now().Unix()))

	t.logger.WithField("url", videoURL).Info("开始下载视频")

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: 10 * time.Minute,
	}

	resp, err := client.Get(videoURL)
	if err != nil {
		return "", fmt.Errorf("下载视频失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载视频失败，状态码: %d", resp.StatusCode)
	}

	// 检查 Content-Length
	if resp.ContentLength > int64(maxSize) {
		return "", fmt.Errorf("视频文件过大: %d bytes > %d bytes limit", resp.ContentLength, maxSize)
	}

	// 创建文件
	file, err := os.Create(videoPath)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer file.Close()

	// 流式下载并检查大小
	written, err := io.Copy(file, &sizeLimiter{reader: resp.Body, limit: int64(maxSize)})
	if err != nil {
		os.Remove(videoPath)
		return "", fmt.Errorf("保存视频失败: %w", err)
	}

	t.logger.WithField("size", written).Info("视频下载完成")
	return videoPath, nil
}

// sizeLimiter 用于限制下载大小
type sizeLimiter struct {
	reader io.Reader
	read   int64
	limit  int64
}

func (sl *sizeLimiter) Read(p []byte) (n int, err error) {
	n, err = sl.reader.Read(p)
	sl.read += int64(n)
	if sl.read > sl.limit {
		return 0, fmt.Errorf("文件大小超过限制: %d", sl.limit)
	}
	return n, err
}

// ExtractAudio 使用 ffmpeg 从视频中提取音频
func (t *TranscribeVideoAction) ExtractAudio(videoPath, feedID string, outputDir string) (string, error) {
	if outputDir == "" {
		outputDir = os.TempDir()
	}
	audioPath := filepath.Join(outputDir, fmt.Sprintf("xhs_%s_%d.mp3", feedID, time.Now().Unix()))

	t.logger.Info("开始提取音频")

	// 使用 ffmpeg 提取音频：转为单声道 MP3，64k 码率
	cmd := exec.Command("ffmpeg",
		"-y",            // 覆盖输出文件
		"-i", videoPath, // 输入文件
		"-vn",         // 禁用视频
		"-b:a", "64k", // 音频码率 64k
		"-ac", "1", // 单声道
		"-ar", "16000", // 采样率 16kHz (Whisper 推荐)
		audioPath, // 输出文件
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("提取音频失败: %w, output: %s", err, string(output))
	}

	// 验证音频文件
	if _, err := os.Stat(audioPath); err != nil {
		return "", fmt.Errorf("音频文件未生成: %w", err)
	}

	t.logger.WithField("audioPath", audioPath).Info("音频提取完成")
	return audioPath, nil
}

// CheckFFmpeg 检查 ffmpeg 是否安装
func CheckFFmpeg() error {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg 未安装或未在 PATH 中，请先安装 ffmpeg")
	}
	return nil
}

// SplitAudio 将大音频文件切片
func (t *TranscribeVideoAction) SplitAudio(audioPath string) ([]string, error) {
	// 获取音频文件信息
	info, err := os.Stat(audioPath)
	if err != nil {
		return nil, err
	}

	// 如果文件小于 20MB，不需要切片
	if info.Size() <= defaultMaxAudioChunk {
		return []string{audioPath}, nil
	}

	t.logger.WithField("size", info.Size()).Info("音频文件较大，进行切片处理")

	// 使用 ffprobe 获取音频时长
	duration, err := t.getAudioDuration(audioPath)
	if err != nil {
		return nil, fmt.Errorf("获取音频时长失败: %w", err)
	}

	// 计算每个切片的时长（目标每个切片约 15MB）
	chunkDuration := int(duration * int(defaultMaxAudioChunk) / int(info.Size()))
	if chunkDuration < 30 {
		chunkDuration = 30 // 最少 30 秒
	}

	var chunks []string
	tmpDir := os.TempDir()

	for start := 0; start < duration; start += chunkDuration {
		end := start + chunkDuration
		if end > duration {
			end = duration
		}

		chunkPath := filepath.Join(tmpDir, fmt.Sprintf("xhs_chunk_%d_%d.mp3", start, time.Now().Unix()))

		// 使用 ffmpeg 切片
		cmd := exec.Command("ffmpeg",
			"-y",
			"-i", audioPath,
			"-ss", fmt.Sprintf("%d", start),
			"-t", fmt.Sprintf("%d", end-start),
			"-c", "copy",
			chunkPath,
		)

		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("音频切片失败: %w, output: %s", err, string(output))
		}

		chunks = append(chunks, chunkPath)
	}

	t.logger.WithField("chunks", len(chunks)).Info("音频切片完成")
	return chunks, nil
}

// getAudioDuration 获取音频时长（秒）
func (t *TranscribeVideoAction) getAudioDuration(audioPath string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		audioPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var duration float64
	fmt.Sscanf(string(output), "%f", &duration)
	return int(duration), nil
}

// TranscribeAudio 使用 Groq Whisper API 转录音频
func (t *TranscribeVideoAction) TranscribeAudio(audioPath, language string) (string, error) {
	apiKey := t.config.GetGroqAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("Groq API Key 未配置")
	}

	// 检查并切分音频
	chunks, err := t.SplitAudio(audioPath)
	if err != nil {
		return "", fmt.Errorf("音频切片失败: %w", err)
	}

	var transcripts []string

	for i, chunk := range chunks {
		t.logger.WithField("chunk", i+1).WithField("total", len(chunks)).Info("转录音频片段")

		transcript, err := t.callGroqWhisperWithRetry(chunk, apiKey, language)
		if err != nil {
			return "", fmt.Errorf("转录音频片段 %d 失败: %w", i+1, err)
		}

		transcripts = append(transcripts, transcript)

		// 删除临时切片文件（如果不是原文件）
		if chunk != audioPath {
			os.Remove(chunk)
		}
	}

	return strings.Join(transcripts, "\n"), nil
}

// callGroqWhisperWithRetry 带重试的 Groq API 调用
func (t *TranscribeVideoAction) callGroqWhisperWithRetry(audioPath, apiKey, language string) (string, error) {
	maxRetries := 3
	baseDelay := time.Second

	for i := 0; i < maxRetries; i++ {
		transcript, err := t.callGroqWhisper(audioPath, apiKey, language)
		if err == nil {
			return transcript, nil
		}

		// 检查是否需要重试
		if i < maxRetries-1 {
			delay := baseDelay * time.Duration(1<<i) // 指数退避: 1s, 2s, 4s
			t.logger.WithError(err).WithField("retry", i+1).WithField("delay", delay).Warn("转录失败，准备重试")
			time.Sleep(delay)
			continue
		}

		return "", err
	}

	return "", fmt.Errorf("超过最大重试次数")
}

// callGroqWhisper 调用 Groq Whisper API
func (t *TranscribeVideoAction) callGroqWhisper(audioPath, apiKey, language string) (string, error) {
	// 打开音频文件
	file, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// 构建 multipart form
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	// 添加文件
	fw, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return "", err
	}

	// 添加其他字段
	w.WriteField("model", "whisper-large-v3")
	if language != "" && language != "auto" {
		w.WriteField("language", language)
	}
	w.WriteField("response_format", "text")

	w.Close()

	// 创建请求
	req, err := http.NewRequest("POST", groqWhisperAPIEndpoint, &b)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	// 发送请求
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 错误 (status %d): %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}
