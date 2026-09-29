package config

import (
	"strings"
	"testing"
)

func TestResolveASRProviderDefaultsToLocal(t *testing.T) {
	t.Parallel()
	cases := map[string]ASRProvider{
		"":         ProviderLocalSTT,
		"  ":       ProviderLocalSTT,
		"local":    ProviderLocalSTT,
		"LOCAL":    ProviderLocalSTT,
		" Local ": ProviderLocalSTT,
	}
	for input, want := range cases {
		got, err := ResolveASRProvider(input)
		if err != nil {
			t.Fatalf("input=%q err=%v", input, err)
		}
		if got != want {
			t.Fatalf("input=%q got=%q want=%q", input, got, want)
		}
	}
}

func TestResolveASRProviderDashScope(t *testing.T) {
	t.Parallel()
	got, err := ResolveASRProvider("dashscope")
	if err != nil {
		t.Fatal(err)
	}
	if got != ProviderDashScopeASR {
		t.Fatalf("got=%q want=%q", got, ProviderDashScopeASR)
	}
}

func TestResolveASRProviderRejectsUnknown(t *testing.T) {
	t.Parallel()
	_, err := ResolveASRProvider("openai")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), EnvASRProvider) {
		t.Fatalf("error=%v should mention %s", err, EnvASRProvider)
	}
}
