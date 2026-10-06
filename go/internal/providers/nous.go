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
	// DefaultNousBaseURL 是 Nous Research 官方 OpenAI-compatible endpoint；本地代理或自建
	// OpenAI-compatible gateway 可通过 RUSHES_NOUS_BASE_URL 覆盖。路径必须以 /v1 结尾，
	// 否则 eino-ext 的 OpenAI client 会按非 chat completions 路径拼接请求。
	DefaultNousBaseURL = "https://inference-api.nousresearch.com/v1"

	// DefaultNousChatModel 是默认聊天模型；可通过 RUSHES_NOUS_CHAT_MODEL 覆盖。
	// 该模型同时声明 image 输入与 tools 支持，因此视觉档可复用同一模型 ID。
	DefaultNousChatModel = "deepseek/deepseek-v4.1-flash"

	// DefaultNousVisionModel 默认为与聊天同一模型；多模态由 Nous gateway 转给上游。
	DefaultNousVisionModel = DefaultNousChatModel

	// DefaultNousTimeout 与现有 OpenRouter 默认对齐，避免快速请求被本地超时切断。
	DefaultNousTimeout = 120 * time.Second
)

// NousConfig 描述 Nous 聊天/视觉单档模型所需的全部设置。
type NousConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

// NousTierConfig 是 NewNousTiers 的输入；密钥缺失时由构造函数在启动期返回简体中文
// 错误，不静默降级，与 ProviderArk、ProviderOpenRouter 行为保持一致。
type NousTierConfig struct {
	APIKey      string
	BaseURL     string
	ChatModel   string
	VisionModel string
	Timeout     time.Duration
}

// NewNousTiers 建立聊天与视觉模型两层；默认使用 eino-ext 的 OpenAI-compatible client，
// 以 Nous BaseURL 调用 chat completions。
//
// 行为细节：
//   - 不附加任何 Qwen 专属参数（如 enable_thinking / chat_template_kwargs）。
//   - HTTP 客户端沿用 NewIPv4Client，并禁用系统代理，便于本地诊断与统一超时。
//   - 视觉模型若与聊天模型相同，Nous 会按模型声明自动转发图像输入。
func NewNousTiers(ctx context.Context, config NousTierConfig) (ModelTiers, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return ModelTiers{}, errors.New("缺少 Nous API 密鑰（RUSHES_NOUS_API_KEY）")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		config.BaseURL = DefaultNousBaseURL
	}
	if strings.TrimSpace(config.ChatModel) == "" {
		config.ChatModel = DefaultNousChatModel
	}
	if strings.TrimSpace(config.VisionModel) == "" {
		config.VisionModel = DefaultNousVisionModel
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultNousTimeout
	}

	chat, err := NewNousChatModel(ctx, NousConfig{
		APIKey: config.APIKey, BaseURL: config.BaseURL, Model: config.ChatModel, Timeout: config.Timeout,
	})
	if err != nil {
		return ModelTiers{}, err
	}
	vision, err := NewNousChatModel(ctx, NousConfig{
		APIKey: config.APIKey, BaseURL: config.BaseURL, Model: config.VisionModel,
		Timeout: config.Timeout + 60*time.Second,
	})
	if err != nil {
		return ModelTiers{}, err
	}
	return ModelTiers{Chat: chat, Vision: vision}, nil
}

// NewNousChatModel 建立单个 Nous 兼容模型。供内部 NewNousTiers 与 worker 视觉档复用。
func NewNousChatModel(ctx context.Context, cfg NousConfig) (model.ToolCallingChatModel, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("缺少 Nous API 密鑰（RUSHES_NOUS_API_KEY）")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultNousBaseURL
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("缺少 Nous 模型 ID（RUSHES_NOUS_CHAT_MODEL 或 RUSHES_NOUS_VISION_MODEL）")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultNousTimeout
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		HTTPClient: NewIPv4Client(cfg.Timeout),
	})
}
