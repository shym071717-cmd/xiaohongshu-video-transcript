package xiaohongshu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
)

// XHSDownloaderClient XHS-Downloader API 客户端
type XHSDownloaderClient struct {
	config     *configs.XHSDownloaderConfig
	httpClient *http.Client
	logger     *logrus.Logger
}

// XHSDetailData XHS-Downloader 详情数据（中文字段）
type XHSDetailData struct {
	Title       string   `json:"作品标题"`
	Desc        string   `json:"作品描述"`
	Author      string   `json:"作者昵称"`
	AuthorID    string   `json:"作者ID"`
	Type        string   `json:"作品类型"`
	WorksID     string   `json:"作品ID"`
	WorksURL    string   `json:"作品链接"`
	DownloadURL []string `json:"下载地址"`
	Images      []string `json:"动图地址"`
	Tags        string   `json:"作品标签"`
	Likes       string   `json:"点赞数量"`
	Collects    string   `json:"收藏数量"`
	Comments    string   `json:"评论数量"`
	Shares      string   `json:"分享数量"`
	PublishTime string   `json:"发布时间"`
}

// XHSDetailResponse XHS-Downloader 详情响应
type XHSDetailResponse struct {
	Message string        `json:"message"`
	Data    XHSDetailData `json:"data"`
}

// XHSDetailRequest XHS-Downloader 详情请求
type XHSDetailRequest struct {
	URL      string `json:"url"`
	Download bool   `json:"download"`
	Cookie   string `json:"cookie,omitempty"`
}

// NewXHSDownloaderClient 创建 XHS-Downloader 客户端
func NewXHSDownloaderClient(config *configs.XHSDownloaderConfig, logger *logrus.Logger) *XHSDownloaderClient {
	if config == nil {
		config = configs.DefaultXHSDownloaderConfig()
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &XHSDownloaderClient{
		config:     config,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		logger:     logger,
	}
}

// GetVideoInfo 获取小红书视频信息
// 参数 xiaohongshuURL: 小红书作品链接，如 https://www.xiaohongshu.com/explore/xxx
func (c *XHSDownloaderClient) GetVideoInfo(xiaohongshuURL string) (*VideoInfo, error) {
	baseURL := c.config.GetBaseURL()
	apiURL := fmt.Sprintf("%s/xhs/detail", baseURL)

	reqBody := XHSDetailRequest{
		URL:      xiaohongshuURL,
		Download: false, // 不下载文件，只获取信息
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	c.logger.WithField("url", xiaohongshuURL).Info("调用 XHS-Downloader API")

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 XHS-Downloader 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("XHS-Downloader API 错误 (status %d): %s", resp.StatusCode, string(body))
	}

	var result XHSDetailResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 验证是否为视频类型（支持中英文）
	if result.Data.Type != "video" && result.Data.Type != "视频" {
		return nil, fmt.Errorf("该作品不是视频类型: %s", result.Data.Type)
	}

	// 提取视频下载地址
	videoURL := c.extractVideoURL(&result.Data)
	if videoURL == "" {
		return nil, fmt.Errorf("未找到视频下载地址")
	}

	c.logger.WithFields(logrus.Fields{
		"title":  result.Data.Title,
		"author": result.Data.Author,
	}).Info("成功获取视频信息")

	return &VideoInfo{
		Title:    result.Data.Title,
		Author:   result.Data.Author,
		VideoURL: videoURL,
	}, nil
}

// extractVideoURL 从响应中提取视频下载地址
func (c *XHSDownloaderClient) extractVideoURL(data *XHSDetailData) string {
	if len(data.DownloadURL) == 0 {
		c.logger.Warn("XHS-Downloader 响应中未包含下载地址")
		return ""
	}

	// 依次尝试每个下载地址，进行解码和验证
	for _, rawURL := range data.DownloadURL {
		if rawURL == "" {
			continue
		}

		// 清理前后空格
		cleanURL := strings.TrimSpace(rawURL)

		// 尝试 URL 解码（处理可能被编码的地址）
		decodedURL, err := url.QueryUnescape(cleanURL)
		if err != nil || decodedURL == "" {
			decodedURL = cleanURL
		}

		// 验证是否为有效的 HTTP/HTTPS URL
		if strings.HasPrefix(decodedURL, "http://") || strings.HasPrefix(decodedURL, "https://") {
			c.logger.WithField("url", decodedURL).Debug("成功提取视频下载地址")
			return decodedURL
		}

		c.logger.WithField("raw_url", rawURL).Warn("跳过无效的视频下载地址")
	}

	c.logger.Error("所有下载地址均无效")
	return ""
}

// HealthCheck 检查 XHS-Downloader 服务健康状态
func (c *XHSDownloaderClient) HealthCheck() error {
	baseURL := c.config.GetBaseURL()
	apiURL := fmt.Sprintf("%s/docs", baseURL)

	resp, err := c.httpClient.Get(apiURL)
	if err != nil {
		return fmt.Errorf("XHS-Downloader 服务不可用: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("XHS-Downloader 服务异常: status %d", resp.StatusCode)
	}

	return nil
}
