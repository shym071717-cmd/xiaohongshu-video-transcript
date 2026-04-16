package main

import (
	"flag"
	"os"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

func init() {
	// 加载 .env 文件（如果存在）
	if err := godotenv.Load(); err != nil {
		// .env 文件不存在也没关系，使用系统环境变量
		logrus.Debug("未找到 .env 文件，使用系统环境变量")
	}
}

func main() {
	var (
		headless bool
		binPath  string // 浏览器二进制文件路径
		port     string
		useStdio bool
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.StringVar(&port, "port", ":18060", "HTTP 模式端口")
	flag.BoolVar(&useStdio, "stdio", false, "使用 stdio 模式（与 Claude 集成）")
	flag.Parse()

	if len(binPath) == 0 {
		binPath = os.Getenv("ROD_BROWSER_BIN")
	}

	configs.InitHeadless(headless)
	configs.SetBinPath(binPath)

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService()

	// 创建应用服务器
	appServer := NewAppServer(xiaohongshuService)

	// 根据模式启动服务器
	if useStdio {
		logrus.Info("启动 stdio 模式服务器")
		if err := appServer.StartStdio(); err != nil {
			logrus.Fatalf("failed to run stdio server: %v", err)
		}
	} else {
		logrus.Infof("启动 HTTP 模式服务器，端口: %s", port)
		if err := appServer.StartHTTP(port); err != nil {
			logrus.Fatalf("failed to run HTTP server: %v", err)
		}
	}
}
