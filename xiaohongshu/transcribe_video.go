package xiaohongshu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
