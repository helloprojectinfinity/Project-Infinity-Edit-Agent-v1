package providers

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestOpenRouterTiersRejectsEmptyKey 模拟用户在 .env 中只设 RUSHES_CHAT_PROVIDER=openrouter
// 但漏填 key：构造函数必须在启动期明确拒绝，避免“看似可用、实际无法呼叫模型”的状态。
func TestOpenRouterTiersRejectsEmptyKey(t *testing.T) {
	if _, err := NewOpenRouterTiers(context.Background(), OpenRouterTierConfig{}); err == nil {
		t.Fatal("缺少 API key 时 NewOpenRouterTiers 必须返回错误")
	}
	if _, err := NewOpenRouterChatModel(context.Background(), OpenRouterConfig{}); err == nil {
		t.Fatal("缺少 API key 时 NewOpenRouterChatModel 必须返回错误")
	}
	if _, err := NewOpenRouterChatModel(context.Background(), OpenRouterConfig{APIKey: "test"}); err == nil {
		t.Fatal("缺少模型 ID 时 NewOpenRouterChatModel 必须返回错误")
	}
}

// TestOpenRouterDefaultsForcesBaseURLAndChatModel 验证默认 base URL 与默认模型 ID 会被
// 注入到最终配置，避免误用空字符串导致请求打到错误路径。
func TestOpenRouterDefaultsForcesBaseURLAndChatModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tiers, err := NewOpenRouterTiers(ctx, OpenRouterTierConfig{APIKey: "test"})
	if err != nil {
		t.Fatalf("意外报错：%v", err)
	}
	if tiers.Chat == nil || tiers.Vision == nil {
		t.Fatal("聊天 / 视觉模型必须同时建立")
	}
	if DefaultOpenRouterBaseURL == "" {
		t.Fatal("DefaultOpenRouterBaseURL 不能为空")
	}
	if DefaultOpenRouterChatModel == "" {
		t.Fatal("DefaultOpenRouterChatModel 不能为空")
	}
	if !strings.HasSuffix(DefaultOpenRouterBaseURL, "/v1") {
		t.Fatalf("默认 base URL 必须以 /v1 结尾：%s", DefaultOpenRouterBaseURL)
	}
}
