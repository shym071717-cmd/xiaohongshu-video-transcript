package configs

import "os"

// XHSDownloaderConfig XHS-Downloader 服务配置
type XHSDownloaderConfig struct {
	BaseURL string `json:"base_url" yaml:"base_url" env:"XHS_DOWNLOADER_URL"`
}

// GetBaseURL 获取 XHS-Downloader 服务基础 URL
func (c *XHSDownloaderConfig) GetBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	if url := os.Getenv("XHS_DOWNLOADER_URL"); url != "" {
		return url
	}
	return "http://localhost:5556"
}

// DefaultXHSDownloaderConfig 返回默认配置
func DefaultXHSDownloaderConfig() *XHSDownloaderConfig {
	return &XHSDownloaderConfig{
		BaseURL: "http://localhost:5556",
	}
}
