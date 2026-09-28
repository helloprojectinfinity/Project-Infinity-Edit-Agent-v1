package providers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

const (
	// DefaultOpenRouterBaseURL 是 OpenRouter 的公開端點；本地代理或自建 OpenAI-compatible
	// gateway 可通过 RUSHES_OPENROUTER_BASE_URL 覆盖。路径必须以 /v1 结尾，否则
	// eino-ext 的 OpenAI client 会按非 chat completions 路径拼接请求。
	DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

	// DefaultOpenRouterChatModel 是默认聊天模型；可通过 RUSHES_OPENROUTER_CHAT_MODEL 覆盖。
	// 选型时优先确认目标模型在 OpenRouter 上同时支持工具调用与图像输入，否则只能填
	// RUSHES_OPENROUTER_VISION_MODEL 拆分使用。
	DefaultOpenRouterChatModel = "deepseek/deepseek-v4.1-flash"

	// DefaultOpenRouterVisionModel 默认为与聊天同一模型；多模态由 OpenRouter 转给上游。
	DefaultOpenRouterVisionModel = DefaultOpenRouterChatModel

	// DefaultOpenRouterTimeout 与现有 Qwen 默认对齐，避免快速请求被本地超时切断。
	DefaultOpenRouterTimeout = 120 * time.Second
)

// OpenRouterConfig 描述 OpenRouter 聊天/视觉单档模型所需的全部设置。
//
// Note：聊天与视觉共用同一组认证与端点；模型 ID 独立填写即可。视觉与聊天模型若填相同
// ID，OpenRouter 会按模型声明决定是否转发多模态请求。
type OpenRouterConfig struct {
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
}

// OpenRouterTierConfig 是 NewOpenRouterTiers 的输入；密钥缺失时由构造函数在启动期
// 返回简体中文错误，不静默降级，与 ProviderArk 行为保持一致。
type OpenRouterTierConfig struct {
	APIKey      string
	BaseURL     string
	ChatModel   string
	VisionModel string
	Timeout     time.Duration
}

// NewOpenRouterTiers 建立聊天与视觉模型两层；默认使用 eino-ext 的 OpenAI-compatible
// client，以 OpenRouter BaseURL 调用 chat completions。
//
// 行为细节：
//   - 不附加任何 Qwen 专属参数（如 enable_thinking / chat_template_kwargs）。
//   - HTTP 客户端沿用 NewIPv4Client，并禁用系统代理，便于本地诊断与统一超时。
//   - 视覺模型若与聊天模型相同，OpenRouter 会在多模态请求中根据模型声明自动转发。
func NewOpenRouterTiers(ctx context.Context, config OpenRouterTierConfig) (ModelTiers, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return ModelTiers{}, errors.New("缺少 OpenRouter API 密钥（RUSHES_OPENROUTER_API_KEY）")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		config.BaseURL = DefaultOpenRouterBaseURL
	}
	if strings.TrimSpace(config.ChatModel) == "" {
		config.ChatModel = DefaultOpenRouterChatModel
	}
	if strings.TrimSpace(config.VisionModel) == "" {
		config.VisionModel = DefaultOpenRouterVisionModel
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultOpenRouterTimeout
	}

	chat, err := NewOpenRouterChatModel(ctx, OpenRouterConfig{
		APIKey: config.APIKey, BaseURL: config.BaseURL, Model: config.ChatModel, Timeout: config.Timeout,
	})
	if err != nil {
		return ModelTiers{}, err
	}
	vision, err := NewOpenRouterChatModel(ctx, OpenRouterConfig{
		APIKey: config.APIKey, BaseURL: config.BaseURL, Model: config.VisionModel,
		Timeout: config.Timeout + 60*time.Second,
	})
	if err != nil {
		return ModelTiers{}, err
	}
	if err != nil {
		return ModelTiers{}, err
	}
	return ModelTiers{Chat: chat, Vision: vision}, nil
}

// NewOpenRouterChatModel 建立单个 OpenRouter 兼容模型。供内部 NewOpenRouterTiers 与测试复用。
func NewOpenRouterChatModel(ctx context.Context, cfg OpenRouterConfig) (model.ToolCallingChatModel, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("缺少 OpenRouter API 密钥（RUSHES_OPENROUTER_API_KEY）")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultOpenRouterBaseURL
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("缺少 OpenRouter 模型 ID（RUSHES_OPENROUTER_CHAT_MODEL 或 RUSHES_OPENROUTER_VISION_MODEL）")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultOpenRouterTimeout
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		HTTPClient: NewIPv4Client(cfg.Timeout),
	})
}
