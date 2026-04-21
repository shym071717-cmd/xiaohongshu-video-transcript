---
name: xhs-transcribe
description: |
  小红书视频语音转录与总结工具。当用户需要转录小红书视频笔记的语音内容时触发。
  适用场景：用户提到"转录小红书视频"、"小红书视频转文字"、"视频语音转文字"、
  "xiaohongshu transcript"、"transcribe xhs video"、"提取视频文案"等。
  按需启动依赖服务，执行转录后自动关闭服务，不常驻后台。
---

# 小红书视频转录 Skill

## 概述

本 skill 封装了小红书视频转录的完整工作流：按需启动依赖服务 → 解析视频链接 → 执行语音转录 → AI 生成摘要与结构化分析 → 保存结果 → 自动关闭服务。

**核心特性**：转录完成后，通过一次 Minimax API 调用同时完成：
1. **内容摘要**（核心观点 + 关键要点）
2. **结构化分析**（知识框架 + 行动建议）
3. **转录文本格式化**（添加标点符号、合理分段、添加小标题）

**设计原则**：服务不常驻后台，只在转录期间运行，节省系统资源。

## 前置配置

本 Skill 需要以下环境变量（可写入项目根目录的 `.env` 文件）：

| 环境变量 | 说明 | 获取方式 |
|----------|------|----------|
| `GROQ_API_KEY` | Groq API 密钥（用于语音识别） | 访问 https://console.groq.com 注册并创建 API Key |
| `LLM_API_KEY` | LLM API Key（用于摘要生成） | Minimax: https://platform.minimaxi.com；Claude: https://console.anthropic.com |
| `LLM_PROVIDER` | 摘要提供商：`claude` / `minimax` / `openai` | 默认为 `claude`，推荐使用 `minimax` 节省 token |
| `LLM_MODEL` | 模型名称（可选） | Minimax 默认 `abab6.5s-chat`，Claude 默认 `claude-sonnet-4-6` |
| `HTTP_PROXY` | HTTP 代理地址（中国大陆访问 Groq 需要） | 如 `http://127.0.0.1:你的代理端口`，根据你的代理软件端口填写 |
| `XHS_DOWNLOADER_URL` | XHS-Downloader 服务地址 | 默认 `http://localhost:5556` |
| `TRANSCRIBE_OUTPUT_DIR` | 转录结果保存目录 | 默认 `./output/`，请确保目录存在 |

> **关于 Minimax 的说明**：视频转录后的摘要、结构化分析和文本格式化属于"文本处理类简单任务"。使用 Minimax 等经济型模型处理，可大幅节省 Claude / GPT-4 等主模型的 token 消耗，让主模型专注于更复杂的推理任务。

## 依赖服务

| 服务 | 端口 | 说明 |
|------|------|------|
| XHS-Downloader API | 5556 | 解析小红书链接，获取视频下载地址 |
| 小红书 MCP HTTP | 18060 | 提供转录工具接口 |

## 前置健康检查（Step 0）

**在启动任何服务之前，先逐项检查环境和依赖。如果有问题，立即停止并提示用户修复，不要继续。**

### 检查清单

```bash
# 1. 检查网络能否访问 Groq API（测试直连）
GROQ_STATUS=$(curl -s -o /dev/null -w "%{http_code}" https://api.groq.com/openai/v1/models \
    -H "Authorization: Bearer $(grep GROQ_API_KEY .env | cut -d= -f2)" 2>/dev/null)

# 2. 检查 ffmpeg
FFMPEG_STATUS=$(ffmpeg -version >/dev/null 2>&1 && echo "OK" || echo "MISSING")

# 3. 检查 Python
PYTHON_STATUS=$(python --version >/dev/null 2>&1 && echo "OK" || echo "MISSING")

# 4. 检查 XHS-Downloader 代码
XHS_CODE_STATUS=$(test -f XHS-Downloader/main.py && echo "OK" || echo "MISSING")

# 5. 检查 .env 配置文件
ENV_STATUS=$(test -f .env && echo "OK" || echo "MISSING")

# 6. 检查 MCP 二进制
MCP_BIN_STATUS=$(test -f xiaohongshu-mcp && echo "OK" || echo "MISSING")

# 7. 检查 transcribe_cmd
CMD_STATUS=$(test -f transcribe_cmd && echo "OK" || echo "MISSING")
```

### 检查结果处理

遍历所有检查项，如果任何一项不是 "OK" 或 HTTP 200，向用户报告具体问题并**停止执行**。

| 检查项 | 失败提示 |
|--------|----------|
| Groq API 网络 (`$GROQ_STATUS` != 200) | **无法连接 Groq API（语音识别服务）。请检查：<br>1. 是否开启了全局代理/VPN<br>2. 代理端口是否正确（检查 HTTP_PROXY 环境变量）<br>3. Groq API Key 是否已过期，访问 https://console.groq.com 确认** |
| ffmpeg (`$FFMPEG_STATUS` = MISSING) | **未检测到 ffmpeg。请安装 ffmpeg 并确保其在 PATH 中。** |
| Python (`$PYTHON_STATUS` = MISSING) | **未检测到 Python。请安装 Python 3.x。** |
| XHS-Downloader 代码 (`$XHS_CODE_STATUS` = MISSING) | **XHS-Downloader 代码缺失。请确认目录存在：`XHS-Downloader/`** |
| .env 配置 (`$ENV_STATUS` = MISSING) | **配置文件 .env 缺失。请在项目根目录创建 .env 文件并填入必要的 API Key。** |
| MCP 二进制 (`$MCP_BIN_STATUS` = MISSING) | **xiaohongshu-mcp 缺失。请重新构建项目：`go build .`** |
| transcribe_cmd (`$CMD_STATUS` = MISSING) | **transcribe_cmd 缺失。请重新构建：`go build ./cmd/transcribe/`** |

**所有检查通过后才能进入下一步。**

## 服务生命周期管理

**核心规则**：本 skill 只关闭由本 skill 启动的服务，不关闭用户手动启动的服务。

### Step 1: 检查服务状态并记录

检查两个服务是否已在运行：

```bash
# 检查 XHS-Downloader
XHS_WAS_RUNNING=0
curl -s http://localhost:5556/docs > /dev/null 2>&1 && XHS_WAS_RUNNING=1

# 检查 MCP HTTP 服务器
MCP_WAS_RUNNING=0
curl -s http://localhost:18060/health > /dev/null 2>&1 && MCP_WAS_RUNNING=1
```

**记录状态变量**，供后续关闭步骤使用：
- `XHS_WAS_RUNNING=1`：服务原本就在运行，**不要关闭**
- `XHS_WAS_RUNNING=0`：服务由本 skill 启动，**转录完成后要关闭**

### Step 2: 按需启动缺失的服务

如果 XHS-Downloader 未运行 (`XHS_WAS_RUNNING=0`)：
```bash
if [ "$XHS_WAS_RUNNING" = "0" ]; then
    XHS_DOWNLOADER_DIR="XHS-Downloader"
    cd "$XHS_DOWNLOADER_DIR" && nohup python main.py api > /tmp/xhs_downloader.log 2>&1 &
    XHS_PID=$!
    sleep 3
fi
```

如果 MCP HTTP 服务器未运行 (`MCP_WAS_RUNNING=0`)：
```bash
if [ "$MCP_WAS_RUNNING" = "0" ]; then
    # 从 .env 文件加载环境变量
    if [ -f .env ]; then
        export $(grep -v '^#' .env | xargs)
    fi
    export XHS_DOWNLOADER_URL=${XHS_DOWNLOADER_URL:-http://localhost:5556}
    nohup ./xiaohongshu-mcp > /tmp/xhs_mcp.log 2>&1 &
    MCP_PID=$!
    sleep 2
fi
```

### Step 3: 从链接解析视频参数

从用户提供的小红书分享链接中提取 `feed_id` 和 `xsec_token`：

```
链接格式: https://www.xiaohongshu.com/explore/{feed_id}?xsec_token={token}
```

使用正则表达式或 URL 解析提取这两个参数。

### Step 4: 执行转录

调用本地转录命令，注意需要传入 LLM 相关环境变量：

```bash
# 从 .env 文件加载环境变量
if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi
export XHS_DOWNLOADER_URL=${XHS_DOWNLOADER_URL:-http://localhost:5556}
./transcribe_cmd "{feed_id}" "{xsec_token}"
```

**重要：如果转录返回 403 Forbidden 错误，说明 Groq API Key 已失效。**
此时需要提示用户：
1. 访问 https://console.groq.com 获取新的 API Key
2. 更新 `.env` 文件中的 `GROQ_API_KEY`

### Step 5: 保存结果

转录成功后，结果自动保存到 `TRANSCRIBE_OUTPUT_DIR` 目录（默认 `./output/`）：

```
${TRANSCRIBE_OUTPUT_DIR}/{feed_id}_transcript.md
```

输出文件格式示例：

```markdown
# {小红书笔记原始标题}

**来源**: {url}
**作者**: @{author}
**时长**: {duration}
**转录时间**: {datetime}
**语音识别**: Groq Whisper (whisper-large-v3)

---

## 内容摘要

### 核心观点
（一句话总结）

### 关键要点
1. ...
2. ...
3. ...

---

## 结构化分析

### 知识框架
（层级知识结构）

### 行动建议
（可执行步骤或学习建议）

---

## 完整转录

（带标点、分段、小标题的格式化文本）
```

> **标题规则**：输出文档的一级标题（`#`）直接使用小红书笔记的原始标题，不添加"转录"、"总结"等额外前缀或后缀。

### Step 6: 关闭由本 skill 启动的服务

**关键规则**：只关闭本 skill 启动的服务，保留用户手动启动的服务。

```bash
# 关闭 XHS-Downloader（仅当本 skill 启动时）
if [ "$XHS_WAS_RUNNING" = "0" ] && [ -n "$XHS_PID" ]; then
    kill $XHS_PID 2>/dev/null || true
    echo "已关闭 XHS-Downloader"
fi

# 关闭 MCP HTTP 服务器（仅当本 skill 启动时）
if [ "$MCP_WAS_RUNNING" = "0" ] && [ -n "$MCP_PID" ]; then
    kill $MCP_PID 2>/dev/null || true
    echo "已关闭 MCP 服务器"
fi
```

## 配置

环境变量从以下位置读取（优先级从高到低）：
1. 当前 shell 环境变量
2. 项目根目录的 `.env` 文件

关键配置项：
- `GROQ_API_KEY`: Groq API 密钥（用于语音识别）
- `HTTP_PROXY`: 代理地址（中国大陆访问 Groq 可能需要）
- `XHS_DOWNLOADER_URL`: XHS-Downloader 服务地址（默认 `http://localhost:5556`）
- `LLM_PROVIDER`: 分析 LLM 提供商（minimax/claude/openai）
- `LLM_API_KEY`: 分析 LLM 的 API Key
- `LLM_MODEL`: 分析 LLM 模型名（如 `abab6.5s-chat`）
- `TRANSCRIBE_OUTPUT_DIR`: 转录结果输出目录（默认 `./output/`）

## 故障排查

| 问题 | 原因 | 解决 |
|------|------|------|
| 403 Forbidden | Groq API Key 失效 | 到 console.groq.com 获取新 Key |
| Connection refused | 服务启动失败 | 检查端口是否被占用 |
| 视频下载失败 | XHS-Downloader 解析异常 | 检查 cookies 是否过期，尝试更新 cookies.json |
| 视频解析报错"该作品不是视频类型" | XHS-Downloader cookies 过期 | 重新登录小红书网页版，导出 cookies.json |
| 分析生成超时 | Minimax API 响应慢 | 已设置 120 秒超时，如仍超时请检查网络/代理 |
| 分析生成失败 | LLM API 余额不足/Key 失效 | 检查对应 LLM 提供商账户状态 |
| 输出缺少结构化分析 | Minimax 返回格式异常 | 检查 Minimax API 是否正常，fallback 机制会保留原始转录 |
