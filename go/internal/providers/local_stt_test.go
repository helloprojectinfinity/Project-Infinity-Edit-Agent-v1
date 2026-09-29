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
	if result.ProviderID != "whisper-large-v3" {
		t.Errorf("provider id=%q want whisper-large-v3", result.ProviderID)
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

func TestLocalSTTAnalysisIdentityIncludesLanguageHint(t *testing.T) {
	t.Parallel()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL:      "http://example",
		ModelVersion: "whisper-large-v3",
		AlignerVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := recognizer.AnalysisIdentity()
	if !strings.HasPrefix(id, "whisper-large-v3:") {
		t.Errorf("identity=%q missing prefix", id)
	}
	if !strings.Contains(id, "v1") {
		t.Errorf("identity=%q missing aligner version", id)
	}
}

func TestLocalSTTEmptyLanguageHintProducesAutoInIdentity(t *testing.T) {
	t.Parallel()
	recognizer, err := NewLocalSTT(LocalSTTConfig{
		BaseURL:        "http://example",
		ModelVersion:   "whisper-large-v3",
		AlignerVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recognizer.AnalysisIdentity(), "auto") {
		t.Errorf("identity=%q should include 'auto' for empty hint", recognizer.AnalysisIdentity())
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

func TestLocalSTTEmptyTextReturnsNoWords(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(localSTTResponse{
			Text:     "",
			Language: "zh",
			Provider: "whisper-large-v3",
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

func TestLocalSTTRequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := NewLocalSTT(LocalSTTConfig{})
	if err == nil {
		t.Fatal("expected error for empty base URL")
	}
}
