package providers

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNousTiersRejectsEmptyKey 模拟用户在 .env 中只设 RUSHES_CHAT_PROVIDER=nous
// 但漏填 key：构造函数必须在启动期明确拒绝，避免“看似可用、实际无法呼叫模型”的状态。
func TestNousTiersRejectsEmptyKey(t *testing.T) {
	if _, err := NewNousTiers(context.Background(), NousTierConfig{}); err == nil {
		t.Fatal("缺少 API key 时 NewNousTiers 必须返回错误")
	}
	if _, err := NewNousChatModel(context.Background(), NousConfig{}); err == nil {
		t.Fatal("缺少 API key 时 NewNousChatModel 必须返回错误")
	}
	if _, err := NewNousChatModel(context.Background(), NousConfig{APIKey: "test"}); err == nil {
		t.Fatal("缺少模型 ID 时 NewNousChatModel 必须返回错误")
	}
}

// TestNousDefaultsForcesBaseURLAndChatModel 验证默认 base URL 与默认模型 ID 会被注入
// 到最终配置，避免误用空字符串导致请求打到错误路径。
func TestNousDefaultsForcesBaseURLAndChatModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tiers, err := NewNousTiers(ctx, NousTierConfig{APIKey: "test"})
	if err != nil {
		t.Fatalf("意外报错：%v", err)
	}
	if tiers.Chat == nil || tiers.Vision == nil {
		t.Fatal("聊天 / 视觉模型必须同时建立")
	}
	if DefaultNousBaseURL == "" {
		t.Fatal("DefaultNousBaseURL 不能为空")
	}
	if DefaultNousChatModel == "" {
		t.Fatal("DefaultNousChatModel 不能为空")
	}
	if !strings.HasSuffix(DefaultNousBaseURL, "/v1") {
		t.Fatalf("默认 base URL 必须以 /v1 结尾：%s", DefaultNousBaseURL)
	}
	if DefaultNousVisionModel != DefaultNousChatModel {
		t.Fatalf("视觉档默认应与聊天档同模型：chat=%s vision=%s", DefaultNousChatModel, DefaultNousVisionModel)
	}
}