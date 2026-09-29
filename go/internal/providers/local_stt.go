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

// AnalysisIdentity is persisted in the transcript cache. Changing the model or
// aligner version invalidates cache rows.
func (recognizer *LocalSTT) AnalysisIdentity() string {
	return recognizer.modelVersion + ":auto:" + recognizer.alignerVersion
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
	if strings.TrimSpace(body.Text) == "" {
		return contracts.SpeechRecognitionResult{}, contracts.ErrSpeechNoWords
	}

	result := contracts.SpeechRecognitionResult{
		Text:       strings.TrimSpace(body.Text),
		Language:   strings.TrimSpace(body.Language),
		ProviderID: strings.TrimSpace(body.Provider),
	}
	if result.ProviderID == "" {
		result.ProviderID = recognizer.modelVersion
	}
	for _, segment := range body.Segments {
		converted := contracts.SpeechRecognitionSegment{
			Text:              strings.TrimSpace(segment.Text),
			BeginMilliseconds: segment.BeginMilliseconds,
			EndMilliseconds:   segment.EndMilliseconds,
		}
		for _, word := range segment.Words {
			converted.Words = append(converted.Words, contracts.SpeechRecognitionWord{
				Text:              word.Text,
				BeginMilliseconds: word.BeginMilliseconds,
				EndMilliseconds:   word.EndMilliseconds,
				Punctuation:       word.Punctuation,
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
	Segments []localSTTSegment `json:"segments"`
}

type localSTTSegment struct {
	Text              string       `json:"text"`
	BeginMilliseconds int          `json:"begin_ms"`
	EndMilliseconds   int          `json:"end_ms"`
	Words             []localSTTWord `json:"words"`
}

type localSTTWord struct {
	Text              string `json:"text"`
	BeginMilliseconds int    `json:"begin_ms"`
	EndMilliseconds   int    `json:"end_ms"`
	Punctuation       string `json:"punctuation,omitempty"`
}
