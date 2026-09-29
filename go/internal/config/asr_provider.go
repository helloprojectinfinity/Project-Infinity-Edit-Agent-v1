package config

import (
	"fmt"
	"strings"
)

// ASRProvider identifies which speech recognizer backend to wire into the agent.
type ASRProvider string

const (
	// ProviderLocalSTT runs on-device Whisper via the local Python service. It
	// is the default when RUSHES_ASR_PROVIDER is unset.
	ProviderLocalSTT ASRProvider = "local"
	// ProviderDashScopeASR uses DashScope's cloud ASR. Requires
	// RUSHES_DASHSCOPE_API_KEY and an ASR model name.
	ProviderDashScopeASR ASRProvider = "dashscope"
)

// EnvASRProvider is the switch that selects the ASR backend.
const EnvASRProvider = "RUSHES_ASR_PROVIDER"

// EnvLocalSTTURL is the HTTP base URL of the local STT service.
const EnvLocalSTTURL = "RUSHES_LOCAL_STT_URL"

// EnvLocalSTTModel names the Whisper checkpoint the service loads. It feeds the
// cache identity, so switching models has to invalidate stored transcripts;
// the value must therefore match the service's own RUSHES_LOCAL_STT_MODEL.
const EnvLocalSTTModel = "RUSHES_LOCAL_STT_MODEL"

// EnvLocalSTTAlignerVersion is the local alignment version. Bump it whenever
// the word-to-frame alignment changes so cached transcripts are rebuilt.
const EnvLocalSTTAlignerVersion = "RUSHES_LOCAL_STT_ALIGNER_VERSION"

// DefaultLocalSTTURL is the in-process base URL used when RUSHES_LOCAL_STT_URL
// is unset. The Python service started by scripts/dev_all.sh listens here.
const DefaultLocalSTTURL = "http://127.0.0.1:8013"

// ResolveASRProvider parses the provider switch. Empty defaults to local.
func ResolveASRProvider(raw string) (ASRProvider, error) {
	switch ASRProvider(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ProviderLocalSTT:
		return ProviderLocalSTT, nil
	case ProviderDashScopeASR:
		return ProviderDashScopeASR, nil
	default:
		return "", fmt.Errorf(
			"%s=%q 非法，合法值为 %s 或 %s",
			EnvASRProvider, raw, ProviderLocalSTT, ProviderDashScopeASR,
		)
	}
}
