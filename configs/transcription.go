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

	// 代理配置（中国大陆访问 Groq 需要）
	HTTPProxy string `json:"http_proxy" yaml:"http_proxy" env:"HTTP_PROXY"`

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

// GetHTTPProxy 获取 HTTP 代理地址
func (c *TranscriptionConfig) GetHTTPProxy() string {
	if c.HTTPProxy != "" {
		return c.HTTPProxy
	}
	// 检查环境变量
	if proxy := os.Getenv("HTTP_PROXY"); proxy != "" {
		return proxy
	}
	if proxy := os.Getenv("http_proxy"); proxy != "" {
		return proxy
	}
	return ""
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

// LoadTranscriptionConfigFromEnv 从环境变量加载配置
func LoadTranscriptionConfigFromEnv() *TranscriptionConfig {
	config := &TranscriptionConfig{}

	// 从环境变量读取配置
	if groqKey := os.Getenv("GROQ_API_KEY"); groqKey != "" {
		config.GroqAPIKey = groqKey
	}
	if proxy := os.Getenv("HTTP_PROXY"); proxy != "" {
		config.HTTPProxy = proxy
	}
	if provider := os.Getenv("LLM_PROVIDER"); provider != "" {
		config.LLMProvider = LLMProvider(provider)
	}
	if apiKey := os.Getenv("LLM_API_KEY"); apiKey != "" {
		config.LLMAPIKey = apiKey
	}
	if model := os.Getenv("LLM_MODEL"); model != "" {
		config.LLMModel = model
	}
	if baseURL := os.Getenv("LLM_BASE_URL"); baseURL != "" {
		config.LLMBaseURL = baseURL
	}

	// 如果没有设置 provider，使用默认值
	if config.LLMProvider == "" {
		config.LLMProvider = LLMProviderClaude
	}

	return config
}
