package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// 本程序是一个命令行转录工具示例。
// 运行前请确保已配置以下环境变量：
//   - GROQ_API_KEY    : Groq API Key（用于视频语音识别）
//   - LLM_API_KEY     : LLM API Key（用于生成摘要，可选）
//   - HTTP_PROXY      : HTTP 代理地址（中国大陆访问 Groq 可能需要，可选）
//   - XHS_DOWNLOADER_URL : XHS-Downloader 服务地址（默认为 http://localhost:5556）
//   - TRANSCRIBE_OUTPUT_PATH : 输出文件保存目录（默认为当前工作目录）

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: transcribe_cmd <feed_id> <xsec_token>")
		os.Exit(1)
	}

	feedID := os.Args[1]
	xsecToken := os.Args[2]

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// 加载配置
	transcriptionConfig := configs.LoadTranscriptionConfigFromEnv()
	downloaderConfig := configs.DefaultXHSDownloaderConfig()

	// 创建转录 action
	action := xiaohongshu.NewTranscribeVideoAction(transcriptionConfig, downloaderConfig, logger)

	// 构建参数
	args := xiaohongshu.TranscribeVideoArgs{
		FeedID:      feedID,
		XsecToken:   xsecToken,
		MaxFileSize: 500 * 1024 * 1024, // 500MB
		Language:    "zh",
		WithSummary: true,
	}

	fmt.Printf("开始转录视频: %s\n", feedID)

	// 执行转录
	result, err := action.Transcribe(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "转录失败: %v\n", err)
		os.Exit(1)
	}

	// 格式化结果
	formatted := action.FormatResult(result)

	// 输出到控制台
	fmt.Println(formatted)

	// 保存到文件
	outputDir := os.Getenv("TRANSCRIBE_OUTPUT_PATH")
	if outputDir == "" {
		outputDir = "."
	}
	outputFile := fmt.Sprintf("xiaohongshu_transcription_%s.md", feedID)
	outputPath := filepath.Join(outputDir, outputFile)

	if err := os.WriteFile(outputPath, []byte(formatted), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "保存文件失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n转录结果已保存到: %s\n", outputPath)
}
