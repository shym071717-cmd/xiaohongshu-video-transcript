package xiaohongshu

import (
	"encoding/json"
	"fmt"
	"regexp"

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
