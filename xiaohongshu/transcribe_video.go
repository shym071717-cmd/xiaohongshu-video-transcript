package xiaohongshu

import (
	"fmt"
	"regexp"

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
