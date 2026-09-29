package contracts

import (
	"context"
	"errors"
)

// ErrSpeechNoWords 表示当前音频分块没有可识别台词。
// 分块编排可以跳过它；鉴权、网络与协议错误仍必须中止并回传上层。
var ErrSpeechNoWords = errors.New("ASR 音频分块没有可识别台词")

// SpeechRecognitionRequest 只携带已经由本地媒体层裁出的短音频文件。
// Provider 不接触草稿、素材路径或时间线语义。
type SpeechRecognitionRequest struct {
	AudioPath string
	Language  string
}

type SpeechRecognitionWord struct {
	Text              string
	BeginMilliseconds int
	EndMilliseconds   int
	Punctuation       string
}

type SpeechRecognitionSegment struct {
	Text              string
	BeginMilliseconds int
	EndMilliseconds   int
	Words             []SpeechRecognitionWord
}

type SpeechRecognitionResult struct {
	Text       string
	Language   string
	Emotion    string
	ProviderID string
	Segments   []SpeechRecognitionSegment
}

// SpeechRecognizer 是口播索引与云端 ASR 之间的最小边界。
// 帧坐标、VAD、缓存和语义检索都留在 Rushes 本地。
type SpeechRecognizer interface {
	Recognize(context.Context, SpeechRecognitionRequest) (SpeechRecognitionResult, error)
}

// SpeechTranscriptProvenance 是 SpeechRecognizer 的可选扩展：只有能确认
// 某条 transcript 确实由自己产出时，才实现它。
//
// ProviderFamily 必须在模型版本与 aligner 版本变化时保持稳定，用来把
// 「本识别器产出的 transcript」与「其他来源的 transcript」区分开。
// 缓存层据此判断能否校验语言：认得出的才校验，认不出的按未知来源处理，
// 绝不能因为读不懂 provider_id 就判定缓存过期。
type SpeechTranscriptProvenance interface {
	ProviderFamily() string
}
