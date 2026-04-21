package xiaohongshu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	config           *configs.TranscriptionConfig
	downloaderConfig *configs.XHSDownloaderConfig
	logger           *logrus.Logger
	downloaderClient *XHSDownloaderClient
}

// NewTranscribeVideoAction 创建转录操作实例
func NewTranscribeVideoAction(config *configs.TranscriptionConfig, downloaderConfig *configs.XHSDownloaderConfig, logger *logrus.Logger) *TranscribeVideoAction {
	if config == nil {
		config = configs.DefaultTranscriptionConfig()
	}
	if downloaderConfig == nil {
		downloaderConfig = configs.DefaultXHSDownloaderConfig()
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &TranscribeVideoAction{
		config:           config,
		downloaderConfig: downloaderConfig,
		logger:           logger,
		downloaderClient: NewXHSDownloaderClient(downloaderConfig, logger),
	}
}

// validateFeedID 验证 feedID 格式
func validateFeedID(feedID string) error {
	if !validFeedIDPattern.MatchString(feedID) {
		return fmt.Errorf("invalid feed_id: must contain only alphanumeric characters, hyphens and underscores")
	}
	return nil
}

// ExtractVideoInfo 通过 XHS-Downloader API 获取视频信息
func (t *TranscribeVideoAction) ExtractVideoInfo(feedID, xsecToken string) (*VideoInfo, error) {
	if err := validateFeedID(feedID); err != nil {
		return nil, err
	}

	// 构建小红书作品链接
	xiaohongshuURL := fmt.Sprintf("https://www.xiaohongshu.com/explore/%s?xsec_token=%s", feedID, xsecToken)

	// 使用 XHS-Downloader 客户端获取视频信息
	videoInfo, err := t.downloaderClient.GetVideoInfo(xiaohongshuURL)
	if err != nil {
		return nil, fmt.Errorf("获取视频信息失败: %w", err)
	}

	return videoInfo, nil
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

	// 创建支持代理的 HTTP 客户端，设置超时
	client := t.createHTTPClient(10 * time.Minute)

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

	// 是否需要清理：下载失败时由 defer 关闭并删除文件，成功时 caller 负责删除
	needCleanup := true
	defer func() {
		if needCleanup {
			file.Close()
			os.Remove(videoPath)
		}
	}()

	// 流式下载并检查大小
	written, err := io.Copy(file, &sizeLimiter{reader: resp.Body, limit: int64(maxSize)})
	if err != nil {
		return "", fmt.Errorf("保存视频失败: %w", err)
	}

	// 下载成功，关闭文件并将清理责任交给 caller
	needCleanup = false
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("关闭文件失败: %w", err)
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
	if info.Size() == 0 {
		return nil, fmt.Errorf("音频文件大小为 0，无法切片")
	}
	chunkDuration := int(float64(duration) * float64(defaultMaxAudioChunk) / float64(info.Size()))
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
			if err := os.Remove(chunk); err != nil {
				t.logger.WithError(err).WithField("path", chunk).Warn("删除临时切片文件失败")
			}
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

// createHTTPClient 创建支持代理的 HTTP 客户端
func (t *TranscribeVideoAction) createHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}

	// 检查是否配置了代理（中国大陆访问 Groq 等需要）
	proxyURL := t.config.GetHTTPProxy()
	if proxyURL != "" {
		parsedURL, err := url.Parse(proxyURL)
		if err != nil {
			t.logger.WithError(err).Warn("代理地址解析失败，将不使用代理")
		} else {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(parsedURL),
			}
			t.logger.WithField("proxy", proxyURL).Debug("使用 HTTP 代理")
		}
	}

	return client
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
	if err := w.WriteField("model", "whisper-large-v3"); err != nil {
		return "", fmt.Errorf("构建表单失败: %w", err)
	}
	if language != "" && language != "auto" {
		if err := w.WriteField("language", language); err != nil {
			return "", fmt.Errorf("构建表单失败: %w", err)
		}
	}
	if err := w.WriteField("response_format", "text"); err != nil {
		return "", fmt.Errorf("构建表单失败: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	// 创建请求
	req, err := http.NewRequest("POST", groqWhisperAPIEndpoint, &b)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	// 创建 HTTP 客户端（支持代理）
	client := t.createHTTPClient(2 * time.Minute)
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

// GenerateSummary 使用 LLM 生成摘要
func (t *TranscribeVideoAction) GenerateSummary(transcript, title string) (string, error) {
	provider, apiKey, model, baseURL := t.config.GetLLMConfig()

	if apiKey == "" {
		return "", fmt.Errorf("LLM API Key 未配置")
	}

	prompt := buildSummaryPrompt(title, transcript)

	switch provider {
	case configs.LLMProviderMinimax:
		return t.callMinimaxAPI(apiKey, model, baseURL, prompt)
	case configs.LLMProviderClaude:
		return t.callClaudeAPI(apiKey, model, baseURL, prompt)
	case configs.LLMProviderOpenAI:
		return t.callOpenAIAPI(apiKey, model, baseURL, prompt)
	default:
		return "", fmt.Errorf("不支持的 LLM 提供商: %s", provider)
	}
}

// buildSummaryPrompt 构建结构化分析提示词（摘要 + 知识框架 + 行动建议）
func buildSummaryPrompt(title, transcript string) string {
	return fmt.Sprintf(`你是一位专业的内容分析师。请对以下视频转录内容进行深度分析，生成结构化报告。

视频标题: %s

转录内容:
%s

请严格按以下格式输出，不要添加任何额外说明：

### 核心观点
用一句话精准概括视频核心内容。

### 关键要点
1. （要点1，包含具体论据或数据支撑）
2. （要点2，包含具体论据或数据支撑）
3. （要点3，包含具体论据或数据支撑）

### 知识框架
将视频内容组织为层级知识结构，例如：
- 主题A
  - 子主题1：具体内容
  - 子主题2：具体内容
- 主题B
  - 子主题1：具体内容

### 行动建议
基于视频内容，给出2-5条具体、可执行的步骤或学习建议。
`, title, transcript)
}

// buildFormatTranscriptPrompt 构建转录文本格式化提示词
func buildFormatTranscriptPrompt(transcript string) string {
	return fmt.Sprintf(`你是一位专业的文本编辑。请对以下语音识别转录文本进行排版优化：

1. 补充缺失的标点符号（逗号、句号、问号、感叹号等）
2. 根据语义进行合理分段，每段不宜过长
3. 为每段添加简洁的小标题（使用 #### 级别）
4. 修正明显的语音识别错误（如将同音错别字修正为正确术语）
5. 保持原文意思不变，只做排版和标点优化

原文：
%s

请直接输出格式化后的文本，不要添加任何额外说明或前言。
`, transcript)
}

// Minimax API 请求和响应结构
type minimaxRequest struct {
	Model       string           `json:"model"`
	Messages    []minimaxMessage `json:"messages"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
}

type minimaxMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type minimaxResponse struct {
	Choices []struct {
		Message minimaxMessage `json:"message"`
	} `json:"choices"`
	BaseResp struct {
		StatusMsg  string `json:"status_msg"`
		StatusCode int    `json:"status_code"`
	} `json:"base_resp"`
}

// callMinimaxAPI 调用 Minimax API
func (t *TranscribeVideoAction) callMinimaxAPI(apiKey, model, baseURL, prompt string) (string, error) {
	if baseURL == "" {
		baseURL = "https://api.minimax.chat/v1/text/chatcompletion_v2"
	}

	reqBody := minimaxRequest{
		Model: model,
		Messages: []minimaxMessage{
			{Role: "system", Content: "你是一个专业的视频内容摘要助手。"},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   2000,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result minimaxResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("Minimax API 错误: %s", result.BaseResp.StatusMsg)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("Minimax API 返回空结果")
	}

	return result.Choices[0].Message.Content, nil
}

// callClaudeAPI 调用 Claude API
func (t *TranscribeVideoAction) callClaudeAPI(apiKey, model, baseURL, prompt string) (string, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1/messages"
	}

	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 2000,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Claude API 错误 (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("Claude API 返回空结果")
	}

	return result.Content[0].Text, nil
}

// FormatTranscript 使用 LLM 格式化转录文本（添加标点、分段、小标题）
func (t *TranscribeVideoAction) FormatTranscript(transcript string) (string, error) {
	provider, apiKey, model, baseURL := t.config.GetLLMConfig()

	if apiKey == "" {
		return "", fmt.Errorf("LLM API Key 未配置")
	}

	prompt := buildFormatTranscriptPrompt(transcript)

	switch provider {
	case configs.LLMProviderMinimax:
		return t.callMinimaxAPI(apiKey, model, baseURL, prompt)
	case configs.LLMProviderClaude:
		return t.callClaudeAPI(apiKey, model, baseURL, prompt)
	case configs.LLMProviderOpenAI:
		return t.callOpenAIAPI(apiKey, model, baseURL, prompt)
	default:
		return "", fmt.Errorf("不支持的 LLM 提供商: %s", provider)
	}
}

// callOpenAIAPI 调用 OpenAI API（占位符）
func (t *TranscribeVideoAction) callOpenAIAPI(apiKey, model, baseURL, prompt string) (string, error) {
	return "", fmt.Errorf("OpenAI API 支持尚未实现")
}

// Transcribe 执行完整的视频转录流程
func (t *TranscribeVideoAction) Transcribe(args TranscribeVideoArgs) (*TranscribeResult, error) {
	// 参数校验
	if err := validateFeedID(args.FeedID); err != nil {
		return nil, err
	}

	// 创建临时目录
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("xhs_transcribe_%s_%d", args.FeedID, time.Now().Unix()))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}

	// 确保清理临时文件
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.logger.WithError(err).Warn("清理临时文件失败")
		}
	}()

	result := &TranscribeResult{
		SourceURL:     fmt.Sprintf("https://www.xiaohongshu.com/explore/%s", args.FeedID),
		TranscribedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	// Step 1: 获取视频信息
	t.logger.Info("Step 1: 获取视频信息")
	videoInfo, err := t.ExtractVideoInfo(args.FeedID, args.XsecToken)
	if err != nil {
		return nil, fmt.Errorf("获取视频信息失败: %w", err)
	}
	result.Title = videoInfo.Title
	result.Author = videoInfo.Author
	result.Duration = videoInfo.Duration

	// Step 2: 下载视频（直接下载到临时目录）
	t.logger.Info("Step 2: 下载视频")
	videoPath, err := t.DownloadVideo(videoInfo.VideoURL, args.FeedID, args.MaxFileSize, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("下载视频失败: %w", err)
	}

	// Step 3: 提取音频（直接输出到临时目录）
	t.logger.Info("Step 3: 提取音频")
	audioPath, err := t.ExtractAudio(videoPath, args.FeedID, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("提取音频失败: %w", err)
	}

	// Step 4: 语音识别
	t.logger.Info("Step 4: 语音识别")
	transcript, err := t.TranscribeAudio(audioPath, args.Language)
	if err != nil {
		return nil, fmt.Errorf("语音识别失败: %w", err)
	}
	result.Transcript = transcript

	// Step 5: AI 摘要与结构化分析（可选）
	if args.WithSummary {
		t.logger.Info("Step 5: 生成AI摘要与结构化分析")
		summary, err := t.GenerateSummary(transcript, videoInfo.Title)
		if err != nil {
			t.logger.WithError(err).Warn("生成摘要失败，仅返回转录文本")
			result.Summary = "（摘要生成失败: " + err.Error() + "）"
		} else {
			result.Summary = summary
		}

		// Step 6: 格式化转录文本（可选，依赖摘要步骤）
		t.logger.Info("Step 6: 格式化转录文本")
		formatted, err := t.FormatTranscript(transcript)
		if err != nil {
			t.logger.WithError(err).Warn("格式化转录文本失败，使用原始转录")
			result.FormattedTranscript = transcript
		} else {
			result.FormattedTranscript = formatted
		}
	} else {
		result.FormattedTranscript = transcript
	}

	return result, nil
}

// FormatResult 将结果格式化为 Markdown
func (t *TranscribeVideoAction) FormatResult(result *TranscribeResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", result.Title))
	sb.WriteString(fmt.Sprintf("**来源**: %s\n", result.SourceURL))
	sb.WriteString(fmt.Sprintf("**作者**: @%s\n", result.Author))
	sb.WriteString(fmt.Sprintf("**时长**: %s\n", formatDuration(result.Duration)))
	sb.WriteString(fmt.Sprintf("**转录时间**: %s\n", result.TranscribedAt))
	sb.WriteString("**语音识别**: Groq Whisper (whisper-large-v3)\n")
	sb.WriteString("\n---\n")

	if result.Summary != "" {
		sb.WriteString("\n## 内容摘要\n\n")
		sb.WriteString(result.Summary)
		sb.WriteString("\n\n---\n")
	}

	sb.WriteString("\n## 完整转录\n\n")
	if result.FormattedTranscript != "" {
		sb.WriteString(result.FormattedTranscript)
	} else {
		sb.WriteString(result.Transcript)
	}

	return sb.String()
}

// formatDuration 格式化时长
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%d秒", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%d分%d秒", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%d小时%d分%d秒", seconds/3600, (seconds%3600)/60, seconds%60)
}
