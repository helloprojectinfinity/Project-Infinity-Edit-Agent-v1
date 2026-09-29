package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanzhi84/Rushes/go/internal/contracts"
)

const (
	localSTTDefaultTimeout = 90 * time.Second
	localSTTMaxUploadBytes = 25 * 1024 * 1024
	localSTTDefaultModel   = "whisper-large-v3"
	localSTTDefaultAligner = "v1"
	// localSTTProviderFamily 在模型与 aligner 版本变化时保持不变，用来识别
	// 「这条 transcript 确实是本识别器产出的」。换成另一套模型时前缀不变，
	// 后面的模型段才变，缓存层才能既认得出归属、又校验得出是否过期。
	localSTTProviderFamily = "whisper"

	// Status values the service reports. Anything else means the reply does not
	// match the contract and must not be treated as a successful transcription.
	localSTTStatusSucceeded = "succeeded"
	localSTTStatusNoSpeech  = "no_speech"
)

// LocalSTTConfig describes the local STT service connection.
type LocalSTTConfig struct {
	BaseURL        string
	Timeout        time.Duration
	ModelVersion   string
	AlignerVersion string
}

// LocalSTT implements contracts.SpeechRecognizer against an on-device STT
// service. The service holds the audio model; this provider only owns the
// HTTP, file lifecycle, and parsing.
type LocalSTT struct {
	baseURL        string
	client         *http.Client
	modelVersion   string
	alignerVersion string
}

// NewLocalSTT constructs the recognizer. BaseURL must point at the local
// service's HTTP root (e.g. http://127.0.0.1:8013).
func NewLocalSTT(config LocalSTTConfig) (*LocalSTT, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("本地 STT 缺少服务地址")
	}
	if config.Timeout <= 0 {
		config.Timeout = localSTTDefaultTimeout
	}
	modelVersion := strings.TrimSpace(config.ModelVersion)
	if modelVersion == "" {
		modelVersion = localSTTDefaultModel
	}
	alignerVersion := strings.TrimSpace(config.AlignerVersion)
	if alignerVersion == "" {
		alignerVersion = localSTTDefaultAligner
	}
	return &LocalSTT{
		baseURL:        strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		client:         NewIPv4Client(config.Timeout),
		modelVersion:   modelVersion,
		alignerVersion: alignerVersion,
	}, nil
}

// ProviderFamily 实现 contracts.SpeechTranscriptProvenance，返回跨模型版本
// 稳定的归属前缀。
func (recognizer *LocalSTT) ProviderFamily() string {
	return localSTTProviderFamily + ":"
}

// AnalysisIdentity is the static part of the cache fingerprint persisted with
// every transcript. The Python service speaks through this identity plus the
// per-request language hint (or "auto"). Changing the model version, aligner
// version, or base URL family invalidates cache rows downstream.
func (recognizer *LocalSTT) AnalysisIdentity() string {
	return recognizer.modelVersion + ":" + recognizer.alignerVersion
}

// LanguageIdentity composes the static identity with the per-request language
// hint. Empty hint maps to "auto". The result is what the recognizer stamps
// on each result, so the cache layer can both recognize its own rows and check
// whether they were produced with the same model, aligner, and language.
func (recognizer *LocalSTT) LanguageIdentity(language string) string {
	hint := strings.TrimSpace(language)
	if hint == "" {
		hint = "auto"
	}
	return recognizer.ProviderFamily() + recognizer.AnalysisIdentity() + ":" + hint
}

// Recognize uploads the audio bytes to the local service and parses the
// response into the contract shape.
func (recognizer *LocalSTT) Recognize(
	ctx context.Context,
	request contracts.SpeechRecognitionRequest,
) (contracts.SpeechRecognitionResult, error) {
	if strings.TrimSpace(request.AudioPath) == "" {
		return contracts.SpeechRecognitionResult{}, errors.New("本地 STT 缺少音频路径")
	}
	data, err := os.ReadFile(request.AudioPath)
	if err != nil {
		return contracts.SpeechRecognitionResult{}, fmt.Errorf("读取音频失败: %w", err)
	}
	if len(data) == 0 {
		return contracts.SpeechRecognitionResult{}, errors.New("本地 STT 音频为空")
	}
	if len(data) > localSTTMaxUploadBytes {
		return contracts.SpeechRecognitionResult{}, fmt.Errorf("本地 STT 音频超过 %d MB", localSTTMaxUploadBytes/(1024*1024))
	}

	body, err := recognizer.upload(ctx, data, filepath.Base(request.AudioPath), strings.TrimSpace(request.Language))
	if err != nil {
		return contracts.SpeechRecognitionResult{}, err
	}
	// Validate the contract before interpreting it. Without the status field a
	// wrong-shaped reply — a proxy error page, a future service version, an
	// empty object — would be read as "the audio had no speech", silently
	// dropping a real failure into an empty transcript.
	switch body.Status {
	case localSTTStatusSucceeded:
	case localSTTStatusNoSpeech:
		return contracts.SpeechRecognitionResult{}, contracts.ErrSpeechNoWords
	default:
		return contracts.SpeechRecognitionResult{}, fmt.Errorf(
			"本地 STT 响应 status=%q 不是 %q 或 %q",
			body.Status, localSTTStatusSucceeded, localSTTStatusNoSpeech,
		)
	}
	if strings.TrimSpace(body.Text) == "" {
		return contracts.SpeechRecognitionResult{}, errors.New("本地 STT 响应 status=succeeded 但没有 text")
	}
	if len(body.Segments) == 0 {
		return contracts.SpeechRecognitionResult{}, errors.New("本地 STT 响应缺少 segments")
	}

	result := contracts.SpeechRecognitionResult{
		Text:       strings.TrimSpace(body.Text),
		Language:   strings.TrimSpace(body.Language),
		ProviderID: recognizer.LanguageIdentity(request.Language),
	}
	for index, segment := range body.Segments {
		if segment.EndMilliseconds < segment.BeginMilliseconds {
			return contracts.SpeechRecognitionResult{}, fmt.Errorf(
				"本地 STT segment %d 时间戳倒置: begin=%d end=%d",
				index, segment.BeginMilliseconds, segment.EndMilliseconds,
			)
		}
		converted := contracts.SpeechRecognitionSegment{
			Text:              strings.TrimSpace(segment.Text),
			BeginMilliseconds: segment.BeginMilliseconds,
			EndMilliseconds:   segment.EndMilliseconds,
		}
		for wordIndex, word := range segment.Words {
			if word.EndMilliseconds < word.BeginMilliseconds {
				return contracts.SpeechRecognitionResult{}, fmt.Errorf(
					"本地 STT segment %d word %d 时间戳倒置: begin=%d end=%d",
					index, wordIndex, word.BeginMilliseconds, word.EndMilliseconds,
				)
			}
			converted.Words = append(converted.Words, contracts.SpeechRecognitionWord{
				Text:              word.Text,
				BeginMilliseconds: word.BeginMilliseconds,
				EndMilliseconds:   word.EndMilliseconds,
				Punctuation:       word.Punctuation,
			})
		}
		converted.Alignment = strings.TrimSpace(segment.Alignment)
		if converted.Alignment == "" {
			converted.Alignment = contracts.SpeechAlignmentWord
		}
		if converted.Alignment == contracts.SpeechAlignmentSegmentOnly {
			// A segment-only segment must never ship its words[] as authoritative
			// boundaries; the recogniser already flagged at least one token as
			// unreliable.
			converted.Words = nil
		}
		for _, raw := range segment.RawWords {
			converted.RawWords = append(converted.RawWords, contracts.RawSpeechWord{
				Text:        raw.Text,
				Punctuation: raw.Punctuation,
				RawStartSec: raw.RawStartSec,
				RawEndSec:   raw.RawEndSec,
			})
		}
		for _, issue := range segment.AlignmentIssues {
			converted.AlignmentIssues = append(converted.AlignmentIssues, contracts.SpeechAlignmentIssue{
				WordIndex:   issue.WordIndex,
				Text:        issue.Text,
				Reason:      issue.Reason,
				BeginMS:     issue.BeginMS,
				EndMS:       issue.EndMS,
				RawStartSec: issue.RawStartSec,
				RawEndSec:   issue.RawEndSec,
			})
		}
		result.Segments = append(result.Segments, converted)
	}
	return result, nil
}

func (recognizer *LocalSTT) upload(
	ctx context.Context, audio []byte, filename, language string,
) (localSTTResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("language", language); err != nil {
		return localSTTResponse{}, fmt.Errorf("构造请求失败: %w", err)
	}
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return localSTTResponse{}, fmt.Errorf("构造音频字段失败: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return localSTTResponse{}, fmt.Errorf("写入音频失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return localSTTResponse{}, fmt.Errorf("关闭 multipart 失败: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, recognizer.baseURL+"/transcribe", body)
	if err != nil {
		return localSTTResponse{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := recognizer.client.Do(request)
	if err != nil {
		return localSTTResponse{}, fmt.Errorf("本地 STT 请求失败: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	rawBody, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return localSTTResponse{}, fmt.Errorf("读取本地 STT 响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return localSTTResponse{}, fmt.Errorf("本地 STT 返回 HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	var decoded localSTTResponse
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		return localSTTResponse{}, fmt.Errorf("本地 STT 响应 JSON 解析失败: %w", err)
	}
	return decoded, nil
}

type localSTTResponse struct {
	Text     string           `json:"text"`
	Language string           `json:"language"`
	Provider string           `json:"provider"`
	Status   string           `json:"status"`
	Segments []localSTTSegment `json:"segments"`
}

type localSTTSegment struct {
	Text              string         `json:"text"`
	BeginMilliseconds int            `json:"begin_ms"`
	EndMilliseconds   int            `json:"end_ms"`
	Alignment         string         `json:"alignment,omitempty"`
	Words             []localSTTWord `json:"words"`
	RawWords          []localSTTRawWord `json:"raw_words,omitempty"`
	AlignmentIssues   []localSTTAlignmentIssue `json:"alignment_issues,omitempty"`
}

type localSTTWord struct {
	Text              string `json:"text"`
	BeginMilliseconds int    `json:"begin_ms"`
	EndMilliseconds   int    `json:"end_ms"`
	Punctuation       string `json:"punctuation,omitempty"`
}

type localSTTRawWord struct {
	Text        string   `json:"text"`
	Punctuation string   `json:"punctuation,omitempty"`
	RawStartSec *float64 `json:"raw_start_sec,omitempty"`
	RawEndSec   *float64 `json:"raw_end_sec,omitempty"`
}

type localSTTAlignmentIssue struct {
	WordIndex   int      `json:"word_index"`
	Text        string   `json:"text"`
	Reason      string   `json:"reason"`
	BeginMS     int      `json:"begin_ms"`
	EndMS       int      `json:"end_ms"`
	RawStartSec *float64 `json:"raw_start_sec,omitempty"`
	RawEndSec   *float64 `json:"raw_end_sec,omitempty"`
}
