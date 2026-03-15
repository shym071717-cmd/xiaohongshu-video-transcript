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
