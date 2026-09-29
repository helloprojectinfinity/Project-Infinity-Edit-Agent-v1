package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanzhi84/Rushes/go/internal/contracts"
)

func TestLocalSTTUploadsAudioAndParsesResponse(t *testing.T) {
	t.Parallel()
	var capturedPath string
	var capturedLanguage string
	var capturedAudio []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transcribe" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		capturedLanguage = r.FormValue("language")
		file, header, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer func() { _ = file.Close() }()
		capturedPath = header.Filename
		capturedAudio, err = io.ReadAll(file)
		if err != nil {
			t.Fatalf("read audio: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(localSTTResponse{
			Text:     "你好世界",
			Language: "zh",
			Provider: "whisper-large-v3",
			Status:   localSTTStatusSucceeded,
			Segments: []localSTTSegment{{
				Text:              "你好世界",
				BeginMilliseconds: 0,
				EndMilliseconds:   1000,
				Words: []localSTTWord{
					{Text: "你好", BeginMilliseconds: 0, EndMilliseconds: 500},
					{Text: "世界", BeginMilliseconds: 500, EndMilliseconds: 1000},
				},
			}},
		})
	}))
	defer server.Close()

	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL: server.URL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("FAKE-AUDIO-BYTES"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := recognizer.Recognize(t.Context(), contracts.SpeechRecognitionRequest{
		AudioPath: audioPath,
		Language:  "zh",
	})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}

	if capturedPath != "clip.wav" {
		t.Errorf("uploaded filename=%q want clip.wav", capturedPath)
	}
	if capturedLanguage != "zh" {
		t.Errorf("language hint=%q want zh", capturedLanguage)
	}
	if !bytes.Equal(capturedAudio, []byte("FAKE-AUDIO-BYTES")) {
		t.Errorf("audio bytes mismatch")
	}
	if result.Text != "你好世界" {
		t.Errorf("text=%q want 你好世界", result.Text)
	}
	if result.Language != "zh" {
		t.Errorf("language=%q want zh", result.Language)
	}
	if result.ProviderID != "whisper:whisper-large-v3:v1:zh" {
		t.Errorf("provider id=%q want whisper:whisper-large-v3:v1:zh", result.ProviderID)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("segments=%d want 1", len(result.Segments))
	}
	seg := result.Segments[0]
	if seg.BeginMilliseconds != 0 || seg.EndMilliseconds != 1000 {
		t.Errorf("segment times=%d-%d want 0-1000", seg.BeginMilliseconds, seg.EndMilliseconds)
	}
	if len(seg.Words) != 2 {
		t.Fatalf("words=%d want 2", len(seg.Words))
	}
	if seg.Words[0].Text != "你好" || seg.Words[0].BeginMilliseconds != 0 {
		t.Errorf("word0=%+v", seg.Words[0])
	}
}

func TestLocalSTTAnalysisIdentityCarriesModelAndAligner(t *testing.T) {
	t.Parallel()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL:         "http://example",
		ModelVersion:    "whisper-large-v3",
		AlignerVersion:  "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := recognizer.AnalysisIdentity()
	if id != "whisper-large-v3:v1" {
		t.Errorf("identity=%q want whisper-large-v3:v1", id)
	}
}

func TestLocalSTTAnalysisIdentityChangesWithAlignerVersion(t *testing.T) {
	t.Parallel()
	stable, err := NewLocalSTT(LocalSTTConfig{
		BaseURL:        "http://example",
		ModelVersion:   "whisper-large-v3",
		AlignerVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	bumped, err := NewLocalSTT(LocalSTTConfig{
		BaseURL:        "http://example",
		ModelVersion:   "whisper-large-v3",
		AlignerVersion: "v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stable.AnalysisIdentity() == bumped.AnalysisIdentity() {
		t.Errorf("aligner bump should change identity: stable=%s bumped=%s",
			stable.AnalysisIdentity(), bumped.AnalysisIdentity())
	}
}

func TestLocalSTTLanguageIdentityTracksHint(t *testing.T) {
	t.Parallel()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL:        "http://example",
		ModelVersion:   "whisper-large-v3",
		AlignerVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"":    "whisper:whisper-large-v3:v1:auto",
		"  ":  "whisper:whisper-large-v3:v1:auto",
		"yue": "whisper:whisper-large-v3:v1:yue",
		"zh":  "whisper:whisper-large-v3:v1:zh",
	}
	for input, want := range cases {
		got := recognizer.LanguageIdentity(input)
		if got != want {
			t.Errorf("LanguageIdentity(%q)=%q want %q", input, got, want)
		}
	}
}

// The family prefix must survive model and aligner bumps, otherwise the cache
// layer can no longer tell "stale row from an older model" apart from "row from
// some other provider" and either rebuilds forever or never invalidates.
func TestLocalSTTProviderFamilySurvivesVersionBumps(t *testing.T) {
	t.Parallel()
	build := func(model, aligner string) *LocalSTT {
		recognizer, err := NewLocalSTT(LocalSTTConfig{
			BaseURL: "http://example", ModelVersion: model, AlignerVersion: aligner,
		})
		if err != nil {
			t.Fatal(err)
		}
		return recognizer
	}
	base := build("whisper-large-v3", "v1")
	if got := base.ProviderFamily(); got != "whisper:" {
		t.Fatalf("ProviderFamily()=%q want %q", got, "whisper:")
	}
	for _, variant := range []*LocalSTT{
		build("whisper-small", "v1"),
		build("whisper-large-v3", "v2"),
	} {
		if variant.ProviderFamily() != base.ProviderFamily() {
			t.Errorf("family changed across versions: %q vs %q",
				variant.ProviderFamily(), base.ProviderFamily())
		}
		if !strings.HasPrefix(base.LanguageIdentity("zh"), variant.ProviderFamily()) {
			t.Errorf("stamped id %q missing family prefix %q",
				base.LanguageIdentity("zh"), variant.ProviderFamily())
		}
	}
}

func TestLocalSTTPropagatesServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model crashed"}`))
	}))
	defer server.Close()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = recognizer.Recognize(t.Context(), contracts.SpeechRecognitionRequest{
		AudioPath: audioPath,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error=%v should mention 500", err)
	}
	if !strings.Contains(err.Error(), "model crashed") {
		t.Errorf("error=%v should include server message", err)
	}
}

func TestLocalSTTPropagatesTimeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL: server.URL,
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = recognizer.Recognize(t.Context(), contracts.SpeechRecognitionRequest{
		AudioPath: audioPath,
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestLocalSTTContextCancelAbortsRequest(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err = recognizer.Recognize(ctx, contracts.SpeechRecognitionRequest{AudioPath: audioPath})
	if err == nil {
		t.Fatal("expected error after cancel, got nil")
	}
}

func TestLocalSTTRejectsMalformedBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = recognizer.Recognize(t.Context(), contracts.SpeechRecognitionRequest{AudioPath: audioPath})
	if err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "JSON") && !strings.Contains(err.Error(), "json") &&
		!strings.Contains(err.Error(), "decode") {
		t.Errorf("error=%v should mention JSON decoding", err)
	}
}

func TestLocalSTTNoSpeechStatusReturnsNoWords(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(localSTTResponse{
			Text:     "",
			Language: "zh",
			Provider: "whisper-large-v3",
			Status:   localSTTStatusNoSpeech,
		})
	}))
	defer server.Close()
	recognizer, err := NewLocalSTT(LocalSTTConfig{BaseURL: server.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = recognizer.Recognize(t.Context(), contracts.SpeechRecognitionRequest{AudioPath: audioPath})
	if !errors.Is(err, contracts.ErrSpeechNoWords) {
		t.Errorf("expected ErrSpeechNoWords, got %v", err)
	}
}

// A reply that does not match the contract must surface as an error. Treating
// it as "no speech" would bury a misconfigured or broken service behind an
// empty transcript the user sees as a silent success.
func TestLocalSTTRejectsUnusableResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "unknown status", body: `{"text":"你好","status":"weird"}`},
		{name: "succeeded without text", body: `{"status":"succeeded","segments":[]}`},
		{name: "succeeded without segments", body: `{"status":"succeeded","text":"你好","segments":[]}`},
		{
			name: "segment timestamps inverted",
			body: `{"status":"succeeded","text":"你好","segments":[` +
				`{"text":"你好","begin_ms":1000,"end_ms":0,"words":[]}]}`,
		},
		{
			name: "word timestamps inverted",
			body: `{"status":"succeeded","text":"你好","segments":[` +
				`{"text":"你好","begin_ms":0,"end_ms":1000,` +
				`"words":[{"text":"你","begin_ms":900,"end_ms":100}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			recognizer, err := NewLocalSTT(LocalSTTConfig{BaseURL: server.URL, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			audioPath := filepath.Join(t.TempDir(), "clip.wav")
			if err := os.WriteFile(audioPath, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = recognizer.Recognize(
				t.Context(), contracts.SpeechRecognitionRequest{AudioPath: audioPath},
			)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if errors.Is(err, contracts.ErrSpeechNoWords) {
				t.Errorf("malformed response misreported as no speech: %v", err)
			}
		})
	}
}

func TestLocalSTTRequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := NewLocalSTT(LocalSTTConfig{})
	if err == nil {
		t.Fatal("expected error for empty base URL")
	}
}

// Regression: the service may now flag a segment as segment_only when one of
// its words has unusable boundaries. The provider must surface the label and
// the alignment issues so the caller can keep the full text and drop the
// word-level guarantee.
func TestLocalSTTSurfacesSegmentOnlyAndIssues(t *testing.T) {
	t.Parallel()
	rawStart := 13.44
	rawEnd := 13.44
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(localSTTResponse{
			Text:     "日本語の「を」を含む文。",
			Language: "ja",
			Status:   localSTTStatusSucceeded,
			Segments: []localSTTSegment{{
				Text:              "日本語の「を」を含む文。",
				BeginMilliseconds: 12000,
				EndMilliseconds:   15000,
				Alignment:         contracts.SpeechAlignmentSegmentOnly,
				Words:             []localSTTWord{},
				RawWords: []localSTTRawWord{{
					Text: "を", RawStartSec: &rawStart, RawEndSec: &rawEnd,
				}},
				AlignmentIssues: []localSTTAlignmentIssue{{
					WordIndex: 3, Text: "を",
					Reason: contracts.SpeechAlignmentIssueZeroDur,
					BeginMS: 13440, EndMS: 13440,
					RawStartSec: &rawStart, RawEndSec: &rawEnd,
				}},
			}},
		})
	}))
	defer server.Close()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL: server.URL, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("FAKE"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := recognizer.Recognize(t.Context(), contracts.SpeechRecognitionRequest{
		AudioPath: audioPath, Language: "ja",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Segments) != 1 || result.Segments[0].Alignment != contracts.SpeechAlignmentSegmentOnly {
		t.Fatalf("alignment not surfaced: %#v", result.Segments)
	}
	if len(result.Segments[0].Words) != 0 {
		t.Errorf("segment_only must not carry fabricated word boundaries, got %#v",
			result.Segments[0].Words)
	}
	if len(result.Segments[0].RawWords) != 1 || result.Segments[0].RawWords[0].Text != "を" {
		t.Errorf("raw tokens were dropped: %#v", result.Segments[0].RawWords)
	}
	if len(result.Segments[0].AlignmentIssues) != 1 ||
		result.Segments[0].AlignmentIssues[0].Reason != contracts.SpeechAlignmentIssueZeroDur {
		t.Errorf("alignment issue missing: %#v", result.Segments[0].AlignmentIssues)
	}
}
