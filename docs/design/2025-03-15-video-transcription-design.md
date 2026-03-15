# 小红书视频转录功能设计文档

**日期**: 2025-03-15
**作者**: Claude Code
**状态**: 待实现

## 1. 背景与目标

### 1.1 背景
当前小红书 MCP 服务器已支持发布视频、搜索笔记、获取详情等功能，但**缺少视频内容转录能力**。用户希望像处理 YouTube、Bilibili、小宇宙播客一样，能够将小红书视频笔记的语音内容转录为文本。

### 1.2 目标
为小红书 MCP 服务器新增视频转录功能，用户只需提供小红书视频笔记地址，即可获得：
1. 完整的语音转录文本
2. AI 生成的内容摘要
3. Markdown 格式输出

## 2. 设计决策

### 2.1 技术方案选择

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 语音识别服务 | Groq Whisper API | 与现有小宇宙播客转录方案一致，速度快，中文效果好 |
| 视频下载方案 | 自研实现（参考 XHS-Downloader） | 无需额外 API Key，与现有 go-rod 技术栈一致 |
| 音频处理 | ffmpeg | 行业标准工具，支持转码和切片 |
| 架构方案 | 纯 MCP 工具扩展 | 用户体验最佳，一次调用完成所有操作 |

### 2.2 与其他平台对比

| 平台 | 转录方式 | 实现复杂度 |
|------|---------|-----------|
| YouTube/Bilibili | 直接下载平台提供的 VTT 字幕 | 简单 |
| 小宇宙播客 | 音频下载 + Groq Whisper | 中等 |
| **小红书** | 视频下载 + 音频提取 + Groq Whisper | 复杂（本项目） |

## 3. 架构设计

### 3.1 数据流

```
用户调用 transcribe_video
    ↓
通过 go-rod 访问视频页面
    ↓
从 window.__INITIAL_STATE__ 提取视频信息
    ↓
捕获视频 CDN 地址（通过 network 监听或页面数据）
    ↓
下载视频到系统临时目录 (os.TempDir())
    ↓
ffmpeg 提取音频
    ↓
Groq Whisper API 转录
    ↓
Claude API 生成摘要
    ↓
返回 Markdown 格式结果
```

### 3.2 组件职责

| 组件 | 职责 |
|------|------|
| `mcp_server.go` | 注册 MCP 工具，定义参数 schema |
| `mcp_handlers.go` | 处理工具调用，参数验证 |
| `service.go` | 业务逻辑编排，结果组装 |
| `xiaohongshu/transcribe_video.go` | 核心转录逻辑（视频下载、音频提取、语音识别） |
| `xiaohongshu/types.go` | 类型定义（TranscribeVideoArgs, TranscribeResult） |
| `configs/transcription.go` | Groq API Key 配置管理（与现有 configs/ 结构保持一致） |

## 4. 详细设计

### 4.1 MCP 工具定义

**工具名称**: `transcribe_video`

**输入参数**:

```go
type TranscribeVideoArgs struct {
    FeedID       string `json:"feed_id" jsonschema:"小红书笔记ID，从Feed列表获取"`
    XsecToken    string `json:"xsec_token" jsonschema:"访问令牌，从Feed列表的xsecToken字段获取"`
    Language     string `json:"language,omitempty" jsonschema:"语音识别语言，可选值：zh(中文)、en(英文)、auto(自动检测，默认)"`
    WithSummary  bool   `json:"with_summary,omitempty" jsonschema:"是否生成AI摘要，默认为true"`
    MaxFileSize  int    `json:"max_file_size,omitempty" jsonschema:"最大允许的视频文件大小(MB)，默认500MB"`
}
```

**输出格式** (Markdown):

```markdown
# 视频标题

**来源**: https://www.xiaohongshu.com/explore/{feed_id}
**作者**: @用户名
**时长**: 5分30秒
**转录时间**: 2024-01-20 15:30:00
**语音识别**: Groq Whisper (whisper-large-v3)

---

## 内容摘要

### 核心观点
视频主要分享了...

### 关键要点
1. 第一点...
2. 第二点...
3. 第三点...

---

## 完整转录

[00:00] 大家好，今天我要分享...
[00:05] 关于旅行的一些心得...
...
```

### 4.2 核心算法流程

#### Step 1: 获取视频信息

```go
func (t *TranscribeVideoAction) extractVideoInfo(page *rod.Page, feedID string) (*VideoInfo, error) {
    // 1. 从 window.__INITIAL_STATE__ 提取笔记数据
    evalResult := page.MustEval(`() => {
        if (window.__INITIAL_STATE__ &&
            window.__INITIAL_STATE__.note &&
            window.__INITIAL_STATE__.note.noteDetailMap) {
            const note = window.__INITIAL_STATE__.note.noteDetailMap["` + feedID + `"];
            return JSON.stringify(note);
        }
        return "";
    }`).String()

    // 2. 解析 JSON 获取笔记信息
    var noteDetail struct {
        Note struct {
            Type   string `json:"type"`   // "video" 或 "normal"
            Title  string `json:"title"`
            Video  *struct {
                Capa struct {
                    Duration int `json:"duration"`
                } `json:"capa"`
                // 视频流信息可能在 videoConsumer 或类似字段中
            } `json:"video"`
            User struct {
                Nickname string `json:"nickname"`
            } `json:"user"`
        } `json:"note"`
    }

    if err := json.Unmarshal([]byte(evalResult), &noteDetail); err != nil {
        return nil, fmt.Errorf("解析笔记数据失败: %w", err)
    }

    // 3. 验证是否为视频笔记
    if noteDetail.Note.Type != "video" || noteDetail.Note.Video == nil {
        return nil, fmt.Errorf("该笔记不是视频笔记，无法转录")
    }

    // 4. 获取视频 CDN 地址
    // 方法A: 从 window.__INITIAL_STATE__ 中提取 videoConsumer 或 media 信息
    // 方法B: 启用 Network 监听，捕获视频流请求
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

// 提取视频 URL 的具体实现
func (t *TranscribeVideoAction) extractVideoURL(page *rod.Page, feedID string) string {
    // 尝试从页面数据中提取
    videoURL := page.MustEval(`() => {
        // 尝试多个可能的路径
        const state = window.__INITIAL_STATE__;
        if (state && state.note && state.note.noteDetailMap) {
            const note = state.note.noteDetailMap["` + feedID + `"];
            // 尝试不同的视频地址字段
            if (note.video && note.video.videoConsumer) {
                return note.video.videoConsumer.originVideoKey ||
                       note.video.videoConsumer.url;
            }
            if (note.video && note.video.media) {
                return note.video.media.stream;
            }
        }
        return "";
    }`).String()

    return videoURL
}
```

#### Step 2: 下载视频

```go
import "path/filepath"

func (t *TranscribeVideoAction) downloadVideo(videoURL, feedID string) (string, error) {
    // 1. 使用 os.TempDir() 获取系统临时目录（跨平台兼容）
    tmpDir := os.TempDir()
    videoPath := filepath.Join(tmpDir, fmt.Sprintf("xhs_%s_%d.mp4", feedID, time.Now().Unix()))

    // 2. 使用 HTTP 客户端流式下载，检查文件大小限制
    // 3. 显示下载进度（日志）
    // 4. 返回本地文件路径
}
```

#### Step 3: 提取音频

```go
import "path/filepath"

func (t *TranscribeVideoAction) extractAudio(videoPath, feedID string) (string, error) {
    // 使用 ffmpeg 提取音频并转码为单声道 MP3，64k 码率
    // ffmpeg -i input.mp4 -b:a 64k -ac 1 output.mp3
    tmpDir := os.TempDir()
    audioPath := filepath.Join(tmpDir, fmt.Sprintf("xhs_%s_%d.mp3", feedID, time.Now().Unix()))
    cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-b:a", "64k", "-ac", "1", audioPath)
    err := cmd.Run()
    return audioPath, err
}
```

#### Step 4: 语音识别

参考小宇宙播客实现：`~/.agent-reach/tools/xiaoyuzhou/transcribe.sh`

```go
func (t *TranscribeVideoAction) transcribeAudio(audioPath string, language string) (string, error) {
    // 1. 检查音频文件大小，如果 > 20MB 则切片处理
    chunks, err := t.splitAudioIfNeeded(audioPath)
    if err != nil {
        return "", err
    }

    // 2. 调用 Groq Whisper API（带限流重试）
    var transcripts []string
    for i, chunk := range chunks {
        transcript, err := t.callGroqWhisperWithRetry(chunk, language)
        if err != nil {
            return "", fmt.Errorf("转录音频片段 %d 失败: %w", i+1, err)
        }
        transcripts = append(transcripts, transcript)
    }

    // 3. 合并多段转录结果
    return strings.Join(transcripts, "\n"), nil
}

// 带退避重试的 Groq API 调用
func (t *TranscribeVideoAction) callGroqWhisperWithRetry(audioPath, language string) (string, error) {
    maxRetries := 3
    baseDelay := time.Second

    for i := 0; i < maxRetries; i++ {
        transcript, err := t.callGroqWhisper(audioPath, language)
        if err == nil {
            return transcript, nil
        }

        // 检查是否为限流错误
        if isRateLimitError(err) {
            delay := baseDelay * time.Duration(1<<i) // 指数退避: 1s, 2s, 4s
            time.Sleep(delay)
            continue
        }

        return "", err
    }

    return "", fmt.Errorf("超过最大重试次数")
}
```

#### Step 5: AI 摘要生成

支持多 LLM 提供商（Claude、Minimax、OpenAI 等）：

```go
func (t *TranscribeVideoAction) generateSummary(transcript, title string) (string, error) {
    provider, apiKey, model, baseURL := t.config.GetLLMConfig()

    prompt := buildSummaryPrompt(title, transcript)

    switch provider {
    case LLMProviderMinimax:
        return t.callMinimaxAPI(apiKey, model, baseURL, prompt)
    case LLMProviderClaude:
        return t.callClaudeAPI(apiKey, model, baseURL, prompt)
    case LLMProviderOpenAI:
        return t.callOpenAIAPI(apiKey, model, baseURL, prompt)
    default:
        return "", fmt.Errorf("unsupported LLM provider: %s", provider)
    }
}

func buildSummaryPrompt(title, transcript string) string {
    return fmt.Sprintf(`
请为以下视频内容生成摘要：

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

// Minimax API 调用示例
func (t *TranscribeVideoAction) callMinimaxAPI(apiKey, model, baseURL, prompt string) (string, error) {
    if baseURL == "" {
        baseURL = "https://api.minimax.chat/v1/text/chatcompletion_v2"
    }

    requestBody := map[string]interface{}{
        "model": model,
        "messages": []map[string]string{
            {"role": "system", "content": "你是一个专业的视频内容摘要助手。"},
            {"role": "user", "content": prompt},
        },
        "temperature": 0.7,
        "max_tokens": 2000,
    }

    // HTTP 调用 Minimax API
    // 实现限流重试机制（同 Groq API）
}

// Claude API 调用示例
func (t *TranscribeVideoAction) callClaudeAPI(apiKey, model, baseURL, prompt string) (string, error) {
    if baseURL == "" {
        baseURL = "https://api.anthropic.com/v1/messages"
    }

    requestBody := map[string]interface{}{
        "model": model,
        "max_tokens": 2000,
        "messages": []map[string]string{
            {"role": "user", "content": prompt},
        },
    }

    // HTTP 调用 Claude API
    // 实现限流重试机制
}
```

### 4.3 错误处理与超时

| 步骤 | 预计耗时 | 超时设置 | 错误处理 |
|------|---------|---------|---------|
| 获取视频信息 | 2-5s | 30s | 检查登录状态，验证是否为视频笔记 |
| 下载视频 | 取决于视频大小 | 10min | 支持断点续传，网络错误重试 |
| 提取音频 | 10-30s | 2min | 检查 ffmpeg 是否安装 |
| 语音识别 | 取决于时长 | 5min | 切片处理大文件，API 错误重试 |
| AI 摘要 | 5-10s | 30s | 可选步骤，失败不影响转录结果 |

### 4.4 资源清理策略

- **自动清理**: 无论成功或失败，临时文件（视频、音频、切片）都会在返回结果前自动删除
- **defer 机制**: 使用 Go 的 defer 确保即使发生 panic 也能清理
- **临时目录**: 所有文件写入系统临时目录（`os.TempDir()`），跨平台兼容（Windows、macOS、Linux），即使程序崩溃也会由系统定期清理

```go
import "path/filepath"

func (t *TranscribeVideoAction) Transcribe(feedID, xsecToken string) (*TranscribeResult, error) {
    // 创建临时目录（使用 os.TempDir() 确保跨平台兼容）
    tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("xhs_transcribe_%s_%d", feedID, time.Now().Unix()))
    os.MkdirAll(tmpDir, 0755)

    // 确保清理（使用 defer 即使 panic 也能执行）
    defer func() {
        os.RemoveAll(tmpDir)
    }()

    // ... 执行转录流程
}
```

## 5. 配置文件

### 5.1 新增配置项

在 `configs/transcription.go` 中添加：

```go
package configs

// LLMProvider 定义支持的 LLM 提供商类型
type LLMProvider string

const (
    LLMProviderClaude  LLMProvider = "claude"
    LLMProviderMinimax LLMProvider = "minimax"
    LLMProviderOpenAI  LLMProvider = "openai"
)

type TranscriptionConfig struct {
    // 语音识别配置
    GroqAPIKey string `json:"groq_api_key" yaml:"groq_api_key" env:"GROQ_API_KEY"`

    // LLM 摘要配置（支持多提供商）
    LLMProvider    LLMProvider `json:"llm_provider" yaml:"llm_provider" env:"LLM_PROVIDER"`        // 默认: claude
    LLMAPIKey      string      `json:"llm_api_key" yaml:"llm_api_key" env:"LLM_API_KEY"`          // LLM API Key
    LLMModel       string      `json:"llm_model" yaml:"llm_model" env:"LLM_MODEL"`              // 模型名称，如 "abab6.5s-chat"
    LLMBaseURL     string      `json:"llm_base_url" yaml:"llm_base_url" env:"LLM_BASE_URL"`      // 可选：自定义 API 基础 URL
}

func (c *TranscriptionConfig) GetGroqAPIKey() string {
    if c.GroqAPIKey != "" {
        return c.GroqAPIKey
    }
    return os.Getenv("GROQ_API_KEY")
}

func (c *TranscriptionConfig) GetLLMConfig() (provider LLMProvider, apiKey, model, baseURL string) {
    provider = c.LLMProvider
    if provider == "" {
        provider = LLMProviderClaude // 默认使用 Claude
    }

    apiKey = c.LLMAPIKey
    if apiKey == "" {
        // 兼容旧版环境变量
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
        }
    }

    baseURL = c.LLMBaseURL
    return
}
```

### 5.2 环境变量（优先级最高）

```bash
# 必须：Groq API Key（用于语音识别）
export GROQ_API_KEY="gsk_xxxxx"

# 可选：LLM 摘要配置
export LLM_PROVIDER="minimax"           # 可选: claude, minimax, openai
export LLM_API_KEY="your-api-key"       # Minimax 或 Claude 的 API Key
export LLM_MODEL="abab6.5s-chat"        # 可选，使用提供商默认模型
export LLM_BASE_URL=""                  # 可选，自定义 API 地址
```

### 5.3 配置文件（可选）

如果项目支持配置文件，可以在配置文件中添加：

```yaml
# config.yaml
transcription:
  groq_api_key: "gsk_xxxxx"

  # LLM 摘要配置示例（Minimax）
  llm_provider: "minimax"
  llm_api_key: "your-minimax-api-key"
  llm_model: "abab6.5s-chat"

  # 或使用 Claude（默认）
  # llm_provider: "claude"
  # llm_api_key: "sk-xxxx"
  # llm_model: "claude-3-5-sonnet-20241022"
```

## 6. 依赖要求

### 6.1 系统依赖

| 依赖 | 用途 | 安装命令 |
|------|------|---------|
| ffmpeg | 音频提取和切片 | `brew install ffmpeg` (Mac) / `apt install ffmpeg` (Linux) |

### 6.2 API 依赖

| 服务 | 用途 | 获取方式 | 备注 |
|------|------|---------|------|
| Groq API | 语音识别 | https://console.groq.com | 必须 |
| Minimax API | AI 摘要（可选） | https://platform.minimaxi.com | 支持 abab6.5s-chat 等模型 |
| Claude API | AI 摘要（可选） | https://console.anthropic.com | Anthropic 官方 API |
| OpenAI API | AI 摘要（可选） | https://platform.openai.com | 兼容 GPT 系列模型 |

## 7. 安全措施

### 7.1 文件路径验证

```go
import "regexp"

var validFeedIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func validateFeedID(feedID string) error {
    if !validFeedIDPattern.MatchString(feedID) {
        return fmt.Errorf("invalid feed_id: must contain only alphanumeric characters, hyphens and underscores")
    }
    return nil
}
```

### 7.2 文件大小限制

```go
const defaultMaxVideoSize = 500 * 1024 * 1024 // 500MB

func checkFileSize(path string, maxSize int) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }
    if info.Size() > int64(maxSize) {
        return fmt.Errorf("file size exceeds limit: %d > %d bytes", info.Size(), maxSize)
    }
    return nil
}
```

### 7.3 API Key 安全

- 配置文件权限设置为 0600（仅所有者可读写）
- 在日志中脱敏 API Key
- 使用环境变量时避免在子进程中泄露

## 8. 测试策略

### 8.1 单元测试

- 测试视频信息提取逻辑
- 测试音频切片算法
- 测试临时文件清理

### 8.2 集成测试

- 测试完整转录流程（使用短视频）
- 测试大视频文件处理（> 20MB）
- 测试各种错误场景（网络错误、API 错误等）

### 8.3 验证步骤

1. 运行 `agent-reach doctor --channel xiaohongshu` 检查配置
2. 测试工具调用：使用 `list_feeds` 获取视频笔记，然后调用 `transcribe_video`
3. 验证输出格式是否符合 Markdown 规范

## 9. 风险评估与缓解

| 风险 | 可能性 | 影响 | 缓解措施 |
|------|--------|------|---------|
| 小红书反爬机制更新 | 中 | 高 | 参考 XHS-Downloader 持续更新适配逻辑 |
| Groq API 限制或不可用 | 低 | 高 | 提供备选语音识别服务（如阿里云） |
| 大视频文件下载超时 | 中 | 中 | 增加超时设置，支持断点续传，添加文件大小限制 |
| ffmpeg 未安装 | 低 | 高 | 在 doctor 命令中检查依赖，提供清晰错误提示 |
| 临时文件残留 | 低 | 低 | 使用 defer 清理，即使 panic 也能执行 |
| 路径遍历攻击 | 低 | 中 | 验证 feedID 只包含合法字符，使用 filepath.Join |

## 10. 后续优化方向

1. **缓存机制**: 对已转录的视频进行缓存，避免重复处理
2. **进度反馈**: 向用户展示转录进度（如"正在下载视频..."）
3. **多语言支持**: 支持更多语言的语音识别
4. **备选 ASR**: 集成多个语音识别服务，自动切换

## 11. 参考资源

1. **小宇宙转录脚本**: `~/.agent-reach/tools/xiaoyuzhou/transcribe.sh`
2. **XHS-Downloader**: https://github.com/JoeanAmier/XHS-Downloader
3. **Groq Whisper API**: https://console.groq.com/docs/speech-text
4. **YouTube/Bilibili 转录**: `~/.claude/skills/agent-reach/references/media-platforms.md`
