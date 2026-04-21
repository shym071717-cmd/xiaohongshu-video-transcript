# 小红书视频转录功能实现计划

> **For agentic workers:** REQUIRED: Use @superpowers:subagent-driven-development (if subagents available) or @superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为小红书 MCP 服务器添加视频转录功能，支持从视频笔记中提取音频并通过语音识别生成转录文本和 AI 摘要。

**Architecture:** 新增 `transcribe_video` MCP 工具，通过 go-rod 获取视频 CDN 地址，使用 ffmpeg 提取音频，调用 Groq Whisper API 进行语音识别，支持多 LLM 提供商（Claude/Minimax/OpenAI）生成摘要。

**Tech Stack:** Go, go-rod, ffmpeg, Groq Whisper API, Minimax/Claude API

---

## 文件结构

| 文件 | 操作 | 说明 |
|------|------|------|
| `configs/transcription.go` | 创建 | 转录配置（Groq API Key、LLM 配置） |
| `xiaohongshu/types.go` | 修改 | 添加 VideoInfo、TranscribeResult、IsVideoNote 业务类型 |
| `xiaohongshu/transcribe_video.go` | 创建 | 核心转录逻辑（视频下载、音频提取、语音识别） |
| `types.go` | 修改 | 添加 TranscribeVideoArgs MCP 参数类型 |
| `mcp_server.go` | 修改 | 注册 transcribe_video 工具 |
| `mcp_handlers.go` | 修改 | 添加 transcribe_video 处理函数 |
| `service.go` | 修改 | 添加 TranscribeVideo 业务方法 |

---

## Chunk 1: 配置和类型定义

### Task 1: 创建转录配置

**Files:**
- Create: `configs/transcription.go`

- [ ] **Step 1: 编写配置代码**

```go
package configs

import "os"

// LLMProvider 定义支持的 LLM 提供商类型
type LLMProvider string

const (
    LLMProviderClaude  LLMProvider = "claude"
    LLMProviderMinimax LLMProvider = "minimax"
    LLMProviderOpenAI  LLMProvider = "openai"
)

// TranscriptionConfig 转录功能配置
type TranscriptionConfig struct {
    // 语音识别配置
    GroqAPIKey string `json:"groq_api_key" yaml:"groq_api_key" env:"GROQ_API_KEY"`

    // LLM 摘要配置
    LLMProvider LLMProvider `json:"llm_provider" yaml:"llm_provider" env:"LLM_PROVIDER"`
    LLMAPIKey   string      `json:"llm_api_key" yaml:"llm_api_key" env:"LLM_API_KEY"`
    LLMModel    string      `json:"llm_model" yaml:"llm_model" env:"LLM_MODEL"`
    LLMBaseURL  string      `json:"llm_base_url" yaml:"llm_base_url" env:"LLM_BASE_URL"`
}

// GetGroqAPIKey 获取 Groq API Key
func (c *TranscriptionConfig) GetGroqAPIKey() string {
    if c.GroqAPIKey != "" {
        return c.GroqAPIKey
    }
    return os.Getenv("GROQ_API_KEY")
}

// GetLLMConfig 获取 LLM 配置
func (c *TranscriptionConfig) GetLLMConfig() (provider LLMProvider, apiKey, model, baseURL string) {
    provider = c.LLMProvider
    if provider == "" {
        provider = LLMProviderClaude
    }

    apiKey = c.LLMAPIKey
    if apiKey == "" {
        apiKey = os.Getenv("LLM_API_KEY")
        if apiKey == "" {
            apiKey = os.Getenv("CLAUDE_API_KEY")
        }
    }

    model = c.LLMModel
    if model == "" {
        switch provider {
        case LLMProviderClaude:
            model = "claude-3-5-sonnet-20241022"
        case LLMProviderMinimax:
            model = "abab6.5s-chat"
        case LLMProviderOpenAI:
            model = "gpt-4o"
        }
    }

    baseURL = c.LLMBaseURL
    return
}

// DefaultTranscriptionConfig 返回默认配置
func DefaultTranscriptionConfig() *TranscriptionConfig {
    return &TranscriptionConfig{
        LLMProvider: LLMProviderClaude,
    }
}
```

- [ ] **Step 2: 验证代码格式**

Run: `go fmt ./configs/transcription.go`

- [ ] **Step 3: 提交**

```bash
git add configs/transcription.go
git commit -m "feat(config): add transcription config with multi-LLM support"
```

---

### Task 2: 添加业务类型定义

**Files:**
- Modify: `xiaohongshu/types.go`

- [ ] **Step 1: 在文件末尾添加以下业务类型**

```go
// VideoInfo 视频信息
type VideoInfo struct {
    Title    string
    Author   string
    Duration int    // 时长（秒）
    VideoURL string // 视频 CDN 地址
}

// TranscribeResult 转录结果
type TranscribeResult struct {
    Title         string
    Author        string
    Duration      int
    Transcript    string
    Summary       string
    SourceURL     string
    TranscribedAt string
}

// IsVideoNote 检查笔记是否为视频类型
func IsVideoNote(noteType string, video *Video) bool {
    return noteType == "video" && video != nil
}
```

- [ ] **Step 2: 验证代码格式**

Run: `go fmt ./xiaohongshu/types.go`

- [ ] **Step 3: 提交**

```bash
git add xiaohongshu/types.go
git commit -m "feat(types): add VideoInfo and TranscribeResult types"
```

---

## Chunk 2: 核心转录逻辑

### Task 3: 创建转录工具函数

**Files:**
- Create: `xiaohongshu/transcribe_video.go`
- 参考: `/path/to/transcribe.sh`

- [ ] **Step 1: 创建文件并添加基础结构和导入**

```go
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

    "xiaohongshu-mcp/configs"
)

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
```

- [ ] **Step 2: 提交基础结构**

```bash
git add xiaohongshu/transcribe_video.go
git commit -m "feat(transcribe): add base structure and validation"
```

---

### Task 4: 实现视频信息提取

**Files:**
- Modify: `xiaohongshu/transcribe_video.go`

- [ ] **Step 1: 添加视频信息提取函数**

```go
// ExtractVideoInfo 从小红书页面提取视频信息
func (t *TranscribeVideoAction) ExtractVideoInfo(page *rod.Page, feedID string) (*VideoInfo, error) {
    if err := validateFeedID(feedID); err != nil {
        return nil, err
    }

    // 从 window.__INITIAL_STATE__ 提取笔记数据
    evalResult, err := page.Eval(`(feedID) => {
        if (window.__INITIAL_STATE__ &&
            window.__INITIAL_STATE__.note &&
            window.__INITIAL_STATE__.note.noteDetailMap) {
            const note = window.__INITIAL_STATE__.note.noteDetailMap[feedID];
            if (note) {
                return JSON.stringify(note);
            }
        }
        return "";
    }`, feedID).String()

    if err != nil {
        return nil, fmt.Errorf("提取页面数据失败: %w", err)
    }

    if evalResult == "" {
        return nil, fmt.Errorf("无法获取笔记数据，请检查 feed_id 和登录状态")
    }

    // 解析 JSON 数据
    var noteDetail struct {
        Note struct {
            Type   string `json:"type"`
            Title  string `json:"title"`
            Video  *Video `json:"video"`
            User   struct {
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
    videoURL, err := page.Eval(`(feedID) => {
        const state = window.__INITIAL_STATE__;
        if (state && state.note && state.note.noteDetailMap) {
            const note = state.note.noteDetailMap[feedID];
            if (note && note.note && note.note.video) {
                const video = note.note.video;
                // 尝试多种可能的字段路径
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
    }`, feedID).String()

    if err != nil {
        t.logger.WithError(err).Error("提取视频URL失败")
        return ""
    }

    return videoURL
}
```

- [ ] **Step 2: 格式化并提交**

```bash
go fmt ./xiaohongshu/transcribe_video.go
git add xiaohongshu/transcribe_video.go
git commit -m "feat(transcribe): add video info extraction"
```

---

### Task 5: 实现视频下载和音频提取

**Files:**
- Modify: `xiaohongshu/transcribe_video.go`

- [ ] **Step 1: 添加视频下载函数**

```go
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
    written, err := io.Copy(file, io.TeeReader(resp.Body, &sizeLimiter{limit: int64(maxSize)}))
    if err != nil {
        os.Remove(videoPath)
        return "", fmt.Errorf("保存视频失败: %w", err)
    }

    t.logger.WithField("size", written).Info("视频下载完成")
    return videoPath, nil
}

// sizeLimiter 用于限制下载大小
type sizeLimiter struct {
    written int64
    limit   int64
}

func (sl *sizeLimiter) Write(p []byte) (n int, err error) {
    sl.written += int64(len(p))
    if sl.written > sl.limit {
        return 0, fmt.Errorf("文件大小超过限制: %d", sl.limit)
    }
    return len(p), nil
}
```

- [ ] **Step 2: 添加音频提取函数**

```go
// ExtractAudio 使用 ffmpeg 从视频中提取音频
func (t *TranscribeVideoAction) ExtractAudio(videoPath, feedID string, outputDir string) (string, error) {
    if outputDir == "" {
        outputDir = os.TempDir()
    }
    audioPath := filepath.Join(outputDir, fmt.Sprintf("xhs_%s_%d.mp3", feedID, time.Now().Unix()))

    t.logger.Info("开始提取音频")

    // 使用 ffmpeg 提取音频：转为单声道 MP3，64k 码率
    cmd := exec.Command("ffmpeg",
        "-y",               // 覆盖输出文件
        "-i", videoPath,   // 输入文件
        "-vn",             // 禁用视频
        "-b:a", "64k",     // 音频码率 64k
        "-ac", "1",        // 单声道
        "-ar", "16000",    // 采样率 16kHz (Whisper 推荐)
        audioPath,         // 输出文件
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
```

- [ ] **Step 3: 格式化并提交**

```bash
go fmt ./xiaohongshu/transcribe_video.go
git add xiaohongshu/transcribe_video.go
git commit -m "feat(transcribe): add video download and audio extraction"
```

---

### Task 6: 实现语音识别（Groq Whisper）

**Files:**
- Modify: `xiaohongshu/transcribe_video.go`

- [ ] **Step 1: 添加音频切片函数**

```go
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
    chunkDuration := int(duration * defaultMaxAudioChunk / int(info.Size()))
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
```

- [ ] **Step 2: 添加 Groq Whisper API 调用函数**

```go
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
```

- [ ] **Step 3: 格式化并提交**

```bash
go fmt ./xiaohongshu/transcribe_video.go
git add xiaohongshu/transcribe_video.go
git commit -m "feat(transcribe): add Groq Whisper speech recognition"
```

---

### Task 7: 实现 AI 摘要生成（多 LLM 支持）

**Files:**
- Modify: `xiaohongshu/transcribe_video.go`

- [ ] **Step 1: 添加 LLM 摘要生成函数**

```go
// GenerateSummary 使用 LLM 生成摘要
func (t *TranscribeVideoAction) GenerateSummary(transcript, title string) (string, error) {
    provider, apiKey, model, baseURL := t.config.GetLLMConfig()

    if apiKey == "" {
        return "", fmt.Errorf("LLM API Key 未配置")
    }

    prompt := buildSummaryPrompt(title, transcript)

    switch provider {
    case LLMProviderMinimax:
        return t.callMinimaxAPI(apiKey, model, baseURL, prompt)
    case LLMProviderClaude:
        return t.callClaudeAPI(apiKey, model, baseURL, prompt)
    case LLMProviderOpenAI:
        return t.callOpenAIAPI(apiKey, model, baseURL, prompt)
    default:
        return "", fmt.Errorf("不支持的 LLM 提供商: %s", provider)
    }
}

// buildSummaryPrompt 构建摘要提示词
func buildSummaryPrompt(title, transcript string) string {
    return fmt.Sprintf(`请为以下视频内容生成摘要：

视频标题: %s

转录内容:
%s

请生成以下格式的摘要:
### 核心观点
(一句话总结视频核心内容)

### 关键要点
1. (要点1)
2. (要点2)
3. (要点3)
`, title, transcript)
}
```

- [ ] **Step 2: 添加 Minimax API 调用**

```go
// callMinimaxAPI 调用 Minimax API
type minimaxRequest struct {
    Model       string              `json:"model"`
    Messages    []minimaxMessage    `json:"messages"`
    Temperature float64             `json:"temperature"`
    MaxTokens   int                 `json:"max_tokens"`
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
```

- [ ] **Step 3: 添加 Claude API 调用（简化版，可按需扩展）**

```go
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

    // 解析响应（简化处理，实际需要根据 Claude API 响应结构调整）
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

// callOpenAIAPI 调用 OpenAI API（占位符，可按需实现）
func (t *TranscribeVideoAction) callOpenAIAPI(apiKey, model, baseURL, prompt string) (string, error) {
    return "", fmt.Errorf("OpenAI API 支持尚未实现")
}
```

- [ ] **Step 4: 格式化并提交**

```bash
go fmt ./xiaohongshu/transcribe_video.go
git add xiaohongshu/transcribe_video.go
git commit -m "feat(transcribe): add multi-LLM summary generation (Minimax/Claude)"
```

---

### Task 8: 实现主转录流程

**Files:**
- Modify: `xiaohongshu/transcribe_video.go`

- [ ] **Step 1: 添加主转录函数**

```go
// Transcribe 执行完整的视频转录流程
func (t *TranscribeVideoAction) Transcribe(page *rod.Page, args TranscribeVideoArgs) (*TranscribeResult, error) {
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
    videoInfo, err := t.ExtractVideoInfo(page, args.FeedID)
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

    // Step 5: AI 摘要（可选）
    if args.WithSummary {
        t.logger.Info("Step 5: 生成AI摘要")
        summary, err := t.GenerateSummary(transcript, videoInfo.Title)
        if err != nil {
            t.logger.WithError(err).Warn("生成摘要失败，仅返回转录文本")
            result.Summary = "（摘要生成失败: " + err.Error() + "）"
        } else {
            result.Summary = summary
        }
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
    sb.WriteString(result.Transcript)

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
```

- [ ] **Step 2: 格式化并提交**

```bash
go fmt ./xiaohongshu/transcribe_video.go
git add xiaohongshu/transcribe_video.go
git commit -m "feat(transcribe): add main transcription workflow"
```

---

## Chunk 3: MCP 集成

### Task 9: 添加 MCP 工具参数类型

**Files:**
- Modify: `types.go` (根目录)

- [ ] **Step 1: 在 types.go 中添加 TranscribeVideoArgs 类型**

在文件中找到其他 Args 类型（如 FeedDetailArgs）附近，添加：

```go
// TranscribeVideoArgs 视频转录参数
type TranscribeVideoArgs struct {
    FeedID      string `json:"feed_id" jsonschema:"小红书笔记ID，从Feed列表获取"`
    XsecToken   string `json:"xsec_token" jsonschema:"访问令牌，从Feed列表的xsecToken字段获取"`
    Language    string `json:"language,omitempty" jsonschema:"语音识别语言，可选值：zh(中文)、en(英文)、auto(自动检测，默认)"`
    WithSummary bool   `json:"with_summary,omitempty" jsonschema:"是否生成AI摘要，默认为true"`
    MaxFileSize int    `json:"max_file_size,omitempty" jsonschema:"最大允许的视频文件大小(MB)，默认500MB"`
}
```

- [ ] **Step 2: 格式化并提交**

```bash
go fmt ./types.go
git add types.go
git commit -m "feat(types): add TranscribeVideoArgs for MCP"
```

---

### Task 10: 注册 MCP 工具

**Files:**
- Modify: `mcp_server.go`

- [ ] **Step 1: 在 mcp_server.go 中找到工具注册区域，添加 transcribe_video 工具**

参考现有工具注册模式（如 `get_feed_detail`），添加：

```go
// 在 mcp_server.go 中注册 transcribe_video 工具
mcp.AddTool(server,
    &mcp.Tool{
        Name:        "transcribe_video",
        Description: "转录小红书视频笔记的语音内容为文本，并生成AI摘要",
        Annotations: &mcp.ToolAnnotations{
            Title:           "Transcribe Video",
            ReadOnlyHint:    boolPtr(true),
            DestructiveHint: boolPtr(false),
        },
    },
    withPanicRecovery("transcribe_video", func(ctx context.Context, req *mcp.CallToolRequest, args TranscribeVideoArgs) (*mcp.CallToolResult, any, error) {
        // 转换参数为 map 并调用处理函数
        argsMap := map[string]interface{}{
            "feed_id":       args.FeedID,
            "xsec_token":    args.XsecToken,
            "language":      args.Language,
            "with_summary":  args.WithSummary,
            "max_file_size": args.MaxFileSize,
        }
        result := appServer.handleTranscribeVideo(ctx, argsMap)
        return convertToMCPResult(result), nil, nil
    }),
)
```

- [ ] **Step 2: 确保导入 xiaohongshu 包**

确认文件顶部有：`import "xiaohongshu-mcp/xiaohongshu"`

- [ ] **Step 3: 格式化并提交**

```bash
go fmt ./mcp_server.go
git add mcp_server.go
git commit -m "feat(mcp): register transcribe_video tool"
```

---

### Task 11: 添加 MCP 处理函数

**Files:**
- Modify: `mcp_handlers.go`

- [ ] **Step 1: 在 mcp_handlers.go 中添加 handleTranscribeVideo 函数**

```go
// handleTranscribeVideo 处理视频转录请求
func (s *AppServer) handleTranscribeVideo(ctx context.Context, args map[string]interface{}) *ToolResult {
    // 解析参数
    feedID, ok := args["feed_id"].(string)
    if !ok || feedID == "" {
        return &ToolResult{
            Status: "error",
            Error:  "feed_id 不能为空",
        }
    }

    xsecToken, _ := args["xsec_token"].(string)
    language, _ := args["language"].(string)
    if language == "" {
        language = "auto"
    }

    withSummary := true
    if v, ok := args["with_summary"].(bool); ok {
        withSummary = v
    }

    maxFileSize := 0 // 0 表示使用默认值
    if v, ok := args["max_file_size"].(int); ok && v > 0 {
        maxFileSize = v * 1024 * 1024 // 转换为字节
    }

    // 检查 ffmpeg
    if err := xiaohongshu.CheckFFmpeg(); err != nil {
        return &ToolResult{
            Status: "error",
            Error:  err.Error(),
        }
    }

    // 检查 Groq API Key
    config := configs.DefaultTranscriptionConfig()
    if config.GetGroqAPIKey() == "" {
        return &ToolResult{
            Status: "error",
            Error:  "Groq API Key 未配置，请设置 GROQ_API_KEY 环境变量",
        }
    }

    // 创建转录操作
    action := xiaohongshu.NewTranscribeVideoAction(config, s.logger)

    // 创建浏览器页面
    page, err := s.browser.NewPage()
    if err != nil {
        return &ToolResult{
            Status: "error",
            Error:  fmt.Sprintf("创建浏览器页面失败: %v", err),
        }
    }
    defer page.Close()

    // 导航到视频页面
    videoURL := fmt.Sprintf("https://www.xiaohongshu.com/explore/%s?xsec_token=%s", feedID, xsecToken)
    if err := page.Navigate(videoURL); err != nil {
        return &ToolResult{
            Status: "error",
            Error:  fmt.Sprintf("访问视频页面失败: %v", err),
        }
    }

    // 等待页面加载
    if err := page.WaitLoad(); err != nil {
        return &ToolResult{
            Status: "error",
            Error:  fmt.Sprintf("等待页面加载失败: %v", err),
        }
    }

    // 执行转录
    result, err := action.Transcribe(page, TranscribeVideoArgs{
        FeedID:      feedID,
        XsecToken:   xsecToken,
        Language:    language,
        WithSummary: withSummary,
        MaxFileSize: maxFileSize,
    })

    if err != nil {
        return &ToolResult{
            Status: "error",
            Error:  fmt.Sprintf("转录失败: %v", err),
        }
    }

    // 格式化结果
    markdown := action.FormatResult(result)

    return &ToolResult{
        Status: "success",
        Data: map[string]interface{}{
            "content": markdown,
        },
    }
}
```

- [ ] **Step 2: 确保导入所需的包**

```go
import (
    // 现有导入...
    "xiaohongshu-mcp/configs"
    "xiaohongshu-mcp/xiaohongshu"
)
```

- [ ] **Step 3: 格式化并提交**

```bash
go fmt ./mcp_handlers.go
git add mcp_handlers.go
git commit -m "feat(mcp): add transcribe_video handler"
```

---

### Task 12: 添加 Service 层方法

**Files:**
- Modify: `service.go`

- [ ] **Step 1: 在 service.go 中添加 TranscribeVideo 方法**

```go
// TranscribeVideo 视频转录
func (s *XiaohongshuService) TranscribeVideo(args TranscribeVideoArgs) (*TranscribeResult, error) {
    // 参数校验
    if args.FeedID == "" {
        return nil, fmt.Errorf("feed_id 不能为空")
    }

    // 检查 ffmpeg
    if err := CheckFFmpeg(); err != nil {
        return nil, err
    }

    // 获取配置
    config := configs.DefaultTranscriptionConfig()

    // 创建转录操作
    action := NewTranscribeVideoAction(config, s.logger)

    // 创建浏览器页面
    page, err := s.browser.NewPage()
    if err != nil {
        return nil, fmt.Errorf("创建浏览器页面失败: %w", err)
    }
    defer page.Close()

    // 导航到视频页面
    videoURL := fmt.Sprintf("https://www.xiaohongshu.com/explore/%s?xsec_token=%s", args.FeedID, args.XsecToken)
    if err := page.Navigate(videoURL); err != nil {
        return nil, fmt.Errorf("访问视频页面失败: %w", err)
    }

    if err := page.WaitLoad(); err != nil {
        return nil, fmt.Errorf("等待页面加载失败: %w", err)
    }

    // 执行转录
    result, err := action.Transcribe(page, args)
    if err != nil {
        return nil, err
    }

    return result, nil
}
```

- [ ] **Step 2: 格式化并提交**

```bash
go fmt ./service.go
git add service.go
git commit -m "feat(service): add TranscribeVideo method"
```

---

## Chunk 4: 构建与验证

### Task 13: 构建项目

**Files:**
- All modified files

- [ ] **Step 1: 格式化所有代码**

```bash
go fmt ./...
```

- [ ] **Step 2: 编译检查**

```bash
go build -o xiaohongshu-mcp.exe ./main.go
```

Expected: 编译成功，无错误

- [ ] **Step 3: 提交格式化变更**

```bash
git add -A
git commit -m "style: format all code"
```

---

### Task 14: 功能验证

- [ ] **Step 1: 检查 ffmpeg 安装**

```bash
ffmpeg -version | head -1
```

Expected: 显示 ffmpeg 版本信息

- [ ] **Step 2: 配置环境变量（本地测试）**

```bash
export GROQ_API_KEY="gsk_xxxxx"
export LLM_PROVIDER="minimax"
export LLM_API_KEY="your-minimax-api-key"
```

- [ ] **Step 3: 启动服务并测试**

```bash
./xiaohongshu-mcp.exe
```

在另一个终端测试：
```bash
# 先获取视频笔记
mcporter call 'xiaohongshu.list_feeds()'

# 然后转录视频
mcporter call 'xiaohongshu.transcribe_video(feed_id: "xxx", xsec_token: "xxx")'
```

---

## 附录：测试清单

### 单元测试要点

- [ ] 验证 feedID 格式校验
- [ ] 验证音频切片逻辑
- [ ] 验证临时文件清理

### 集成测试要点

- [ ] 完整转录流程（短视频 < 1分钟）
- [ ] 大视频文件处理（> 20MB 音频需要切片）
- [ ] 错误场景：非视频笔记
- [ ] 错误场景：ffmpeg 未安装
- [ ] 错误场景：Groq API Key 无效

### 手动测试步骤

1. 运行 `agent-reach doctor --channel xiaohongshu` 检查配置
2. 使用 `list_feeds` 获取一个视频笔记
3. 调用 `transcribe_video` 进行转录
4. 验证输出 Markdown 格式
5. 验证临时文件已清理

---

**Plan complete and saved to `docs/plans/2025-03-15-video-transcription.md`. Ready to execute?**
