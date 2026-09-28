package config

import (
	"fmt"
	"strings"
)

// ChatProvider 标识聊天/视觉两档模型使用的厂商。
type ChatProvider string

const (
	// ProviderDashScope 是默认厂商（阿里云百炼 / DashScope）；不设开关时保持零行为变化。
	ProviderDashScope ChatProvider = "dashscope"
	// ProviderArk 是火山方舟（字节 Ark），作为 DashScope 故障时的人工切换备选。
	ProviderArk ChatProvider = "ark"
	// ProviderOpenRouter 通过 OpenRouter 接入任意 OpenAI-compatible 模型；运行时
	// 不绑定 Qwen 专属参数、不会自动装配 DashScope ASR。需显式填写
	// RUSHES_OPENROUTER_API_KEY，未配置时启动期拒绝以“看似可用、实际无法呼叫模型”的状态运行。
	ProviderOpenRouter ChatProvider = "openrouter"
)

// EnvChatProvider 是选择聊天/视觉模型厂商的开关变量名。
const EnvChatProvider = "RUSHES_CHAT_PROVIDER"

// ResolveChatProvider 解析厂商开关：空值默认 dashscope（不设变量时零行为变化），
// 只接受 dashscope、ark 或 openrouter；非法值在启动期报错并列出合法值。返回的错误
// 只含厂商名，不涉及任何密钥。
func ResolveChatProvider(raw string) (ChatProvider, error) {
	switch ChatProvider(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ProviderDashScope:
		return ProviderDashScope, nil
	case ProviderArk:
		return ProviderArk, nil
	case ProviderOpenRouter:
		return ProviderOpenRouter, nil
	default:
		return "", fmt.Errorf(
			"%s=%q 非法，合法值为 %s、%s 或 %s",
			EnvChatProvider, raw, ProviderDashScope, ProviderArk, ProviderOpenRouter,
		)
	}
}
