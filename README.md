# xiaohongshu-video-transcript

基于 [xiaohongshu-mcp](https://github.com/xpzouying/xiaohongshu-mcp) 增强的小红书 MCP 服务器，核心新增**视频语音转录与 AI 结构化分析**功能。

## 核心特性

- **视频语音转录**：将小红书视频笔记的语音内容转录为文本，支持自动语言识别
- **AI 结构化分析**：通过 LLM 生成内容摘要（核心观点 + 关键要点）、知识框架和行动建议
- **Claude Skill 工作流**：内置 `xhs-transcribe` Skill，一键完成健康检查 → 启动服务 → 转录 → 分析 → 保存 → 关闭服务
- **经济型模型设计**：摘要生成推荐使用 Minimax 等经济型模型，大幅节省主模型 token
- **完整 MCP 功能**：保留原项目的发布、搜索、互动等全部小红书运营能力

## 基于原项目

本项目在 [xpzouying/xiaohongshu-mcp](https://github.com/xpzouying/xiaohongshu-mcp) 基础上，新增了视频转录相关功能。原项目的 LICENSE（MIT）和作者版权信息完整保留。

---

## 功能列表

| # | 功能 | 说明 |
|---|------|------|
| 1 | 登录和检查登录状态 | 小红书网页端登录，保存 cookies |
| 2 | 发布图文内容 | 支持本地图片路径和 HTTP 链接 |
| 3 | 发布视频内容 | 支持本地视频文件上传 |
| 4 | 搜索内容 | 关键词搜索，支持多维度筛选 |
| 5 | 获取推荐列表 | 获取小红书首页推荐 Feed |
| 6 | 获取帖子详情 | 含互动数据和评论列表 |
| 7 | 发表评论 | 自动定位输入框并发布 |
| 8 | 获取用户主页 | 含基本信息和笔记列表 |
| 9 | 回复评论 | 精准回复指定评论 |
| 10 | 点赞/取消点赞 | 智能检测状态，避免重复操作 |
| 11 | 收藏/取消收藏 | 智能检测状态，避免重复操作 |
| **12** | **视频转录** | **语音转文本 + AI 结构化分析（本项目新增）** |

### 视频转录详细说明

**输出内容**：

1. **内容摘要**
   - 核心观点：一句话总结视频主旨
   - 关键要点：分点列出重要内容

2. **结构化分析**
   - 知识框架：层级化的知识结构
   - 行动建议：可执行的步骤或学习建议

3. **完整转录**
   - 带标点符号、合理分段、添加小标题的格式化文本

**技术依赖**：
- [XHS-Downloader](https://github.com/joeanamier/xhs-downloader) 解析视频信息
- [Groq Whisper API](https://groq.com/) 语音识别
- LLM（Claude / Minimax / OpenAI）生成摘要和结构化分析

---

## 快速开始

### 1. 下载预编译二进制文件

从 [GitHub Releases](https://github.com/shym071717-cmd/xiaohongshu-video-transcript/releases) 下载对应平台的二进制文件：

| 平台 | 主程序 | 登录工具 |
|------|--------|----------|
| macOS Apple Silicon | `xiaohongshu-mcp-darwin-arm64` | `xiaohongshu-login-darwin-arm64` |
| macOS Intel | `xiaohongshu-mcp-darwin-amd64` | `xiaohongshu-login-darwin-amd64` |
| Windows x64 | `xiaohongshu-mcp-windows-amd64.exe` | `xiaohongshu-login-windows-amd64.exe` |
| Linux x64 | `xiaohongshu-mcp-linux-amd64` | `xiaohongshu-login-linux-amd64` |

```bash
# 1. 运行登录工具
./xiaohongshu-login-darwin-arm64

# 2. 启动 MCP 服务
./xiaohongshu-mcp-darwin-arm64
```

> 首次运行会自动下载无头浏览器（约 150MB）。

### 2. 源码编译

```bash
# 配置 Go 代理（中国大陆推荐）
go env -w GOPROXY=https://goproxy.cn,direct

# 编译
go build .

# 启动
go run .
```

### 3. Docker 部署（含视频转录依赖）

```bash
# 使用 Docker Compose 一键启动 MCP + XHS-Downloader
docker compose up -d
```

该配置会自动：
- 启动 `xhs-downloader` 服务（端口 5556）
- 启动 `xiaohongshu-mcp` 服务（端口 18060）

> 首次启动前，请创建 `.env` 文件并配置 API Key（见下文）。

---

## 环境变量配置

视频转录功能需要以下环境变量（写入项目根目录 `.env` 文件）：

| 环境变量 | 说明 | 是否必填 |
|----------|------|----------|
| `GROQ_API_KEY` | 用于语音识别的 Groq API Key | **是**（视频转录必需） |
| `LLM_API_KEY` | 用于生成摘要的 LLM API Key | 否 |
| `LLM_PROVIDER` | 摘要提供商：`claude`、`minimax`、`openai` | 否 |
| `LLM_MODEL` | 模型名称（可选） | 否 |
| `XHS_DOWNLOADER_URL` | XHS-Downloader 地址，默认 `http://localhost:5556` | 否 |
| `HTTP_PROXY` | HTTP 代理地址，中国大陆访问 Groq 可能需要 | 否 |
| `TRANSCRIBE_OUTPUT_DIR` | 转录结果保存目录，默认 `./output/` | 否 |

> 完整示例请参考 [`.env.example`](./.env.example)。

### 视频转录配置详解

#### Groq API Key（语音识别必需）

1. 访问 [Groq 控制台](https://console.groq.com)
2. 注册/登录 → 左侧 "API Keys" → "Create API Key"
3. 复制生成的 Key（以 `gsk_` 开头）
4. 写入 `.env`：`GROQ_API_KEY=gsk_你的密钥`

> Groq 提供免费额度，个人使用通常足够。中国大陆可能需要配置 `HTTP_PROXY`。

#### LLM API Key（摘要生成可选）

| 提供商 | 获取地址 | 推荐模型 | 特点 |
|--------|----------|----------|------|
| **Minimax** | [platform.minimaxi.com](https://platform.minimaxi.com) | `abab6.5s-chat` | 经济高效，推荐 |
| **Claude** | [console.anthropic.com](https://console.anthropic.com) | `claude-sonnet-4-6` | 质量高，成本高 |
| **OpenAI** | [platform.openai.com](https://platform.openai.com) | `gpt-4o` | 通用性强 |

**为什么推荐 Minimax？**

视频转录后的摘要生成、结构化分析、文本格式化属于"文本处理类简单任务"——不需要复杂推理，但需要消耗大量 token 处理长文本。使用 Minimax 等经济型模型可以：

- **大幅节省主模型 token**：让 Claude / GPT-4 专注于复杂推理
- **降低成本**：Minimax token 价格显著低于 Claude / GPT-4
- **速度更快**：经济型模型响应通常更快
- **质量足够**：文本分段、添加标点、提取要点等任务完全胜任

**配置示例**：
```bash
# .env
GROQ_API_KEY=gsk_你的Groq密钥
LLM_PROVIDER=minimax
LLM_API_KEY=你的Minimax密钥
LLM_MODEL=abab6.5s-chat
HTTP_PROXY=http://127.0.0.1:你的代理端口
```

---

## MCP 客户端接入

服务运行在 `http://localhost:18060/mcp`，支持标准 MCP 协议。

### Claude Code CLI

```bash
claude mcp add --transport http xiaohongshu-mcp http://localhost:18060/mcp
claude mcp list
```

### Cursor

在项目根目录创建 `.cursor/mcp.json`：

```json
{
  "mcpServers": {
    "xiaohongshu-mcp": {
      "url": "http://localhost:18060/mcp",
      "description": "小红书 MCP 服务"
    }
  }
}
```

### VSCode

在项目根目录创建 `.vscode/mcp.json`：

```json
{
  "servers": {
    "xiaohongshu-mcp": {
      "url": "http://localhost:18060/mcp",
      "type": "http"
    }
  }
}
```

### 其他客户端

- **Cline**: `url` + `type: streamableHttp`
- **Gemini CLI**: `~/.gemini/settings.json` 中配置 `mcpServers`
- **MCP Inspector**: `npx @modelcontextprotocol/inspector`，连接 `http://localhost:18060/mcp`

---

## Claude Skill 使用（视频转录工作流自动化）

本项目内置 `xhs-transcribe` Claude Skill，一键完成视频转录完整工作流。

**Skill 功能**：
- 前置健康检查（环境、依赖、API Key 有效性）
- 按需自动启动 XHS-Downloader 和 MCP 服务
- 执行语音转录（Groq Whisper）
- AI 生成结构化分析（摘要 + 知识框架 + 行动建议）
- 自动保存结果到本地文件
- 转录完成后自动关闭由 Skill 启动的服务

**安装**：

```bash
# macOS/Linux
cp -r skills/xhs-transcribe ~/.claude/skills/

# Windows
xcopy /E /I skills\xhs-transcribe %USERPROFILE%\.claude\skills\xhs-transcribe
```

**使用**：

在 Claude Code 中输入：
```
转录这个小红书视频：https://www.xiaohongshu.com/explore/xxx
```

> Skill 遵循"不常驻后台"原则——只在转录期间启动依赖服务，完成后自动关闭。已手动启动的服务不会被关闭。

---

## 可用 MCP 工具

- `check_login_status` - 检查登录状态
- `get_login_qrcode` - 获取登录二维码
- `delete_cookies` - 删除 cookies，重置登录状态
- `publish_content` - 发布图文（title, content, images）
- `publish_with_video` - 发布视频（title, content, video）
- `list_feeds` - 获取首页推荐列表
- `search_feeds` - 搜索内容（keyword, filters）
- `get_feed_detail` - 获取帖子详情（feed_id, xsec_token）
- `post_comment_to_feed` - 发表评论
- `reply_comment_in_feed` - 回复评论
- `like_feed` - 点赞/取消点赞
- `favorite_feed` - 收藏/取消收藏
- `user_profile` - 获取用户主页
- `transcribe_video` - **视频转录**（feed_id, xsec_token, language, with_summary, max_file_size）

---

## 使用示例

### 发布图文

```
帮我写一篇关于春天的帖子发布到小红书上，
使用这些本地图片：
- /Users/username/Pictures/spring_flowers.jpg

使用 xiaohongshu-mcp 进行发布。
```

### 搜索内容

```
搜索小红书上关于"美食"的内容
```

### 转录视频

```
转录这个小红书视频的语音内容：
https://www.xiaohongshu.com/explore/abc123?xsec_token=xxx
```

---

## 小红书运营注意事项

- **标题**：不超过 20 个字
- **正文**：不超过 1000 个字
- **账号安全**：小红书同一账号不允许在多个网页端同时登录，使用 MCP 时不要在其他网页端登录
- **内容合规**：避免违禁词、引流、纯搬运内容
- **实名认证**：新号通常会触发实名认证提醒，建议提前完成

---

## 常见问题

**Q:** 为什么检查登录用户名显示 `xiaghgngshu-mcp`？
**A:** 用户名是写死的占位符。

**Q:** 发布成功但实际没显示？
**A:** 尝试非无头模式重新发布；更换不同内容；检查是否被风控限制网页版发布；确认图片路径无中文；确认图片链接可访问。

**Q:** MCP 程序闪退？
**A:** 建议从源码编译安装，或使用 Docker 部署。

**Q:** Docker 环境下无法连接 MCP？
**A:** 使用 `http://host.docker.internal:18060/mcp` 替代 `localhost`。

**Q:** 视频转录报 403 Forbidden？
**A:** Groq API Key 已失效，访问 https://console.groq.com 获取新 Key。

**Q:** 视频解析报错"该作品不是视频类型"？
**A:** XHS-Downloader cookies 过期，需要重新登录小红书网页版并导出 cookies。

---

## 第三方引用与致谢

- 基于 [xpzouying/xiaohongshu-mcp](https://github.com/xpzouying/xiaohongshu-mcp) 开发，保留原项目 MIT License 和作者版权
- 视频解析基于 [XHS-Downloader](https://github.com/joeanamier/xhs-downloader)
- 语音识别使用 [Groq Whisper API](https://groq.com/)
- 摘要生成支持 [Minimax](https://platform.minimaxi.com)、[Claude](https://anthropic.com)、[OpenAI](https://openai.com)
- 基于 [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) 规范构建

## License

[MIT License](./LICENSE)

Copyright (c) 2025 xpzouying
