package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMapLocalSTTStatus(t *testing.T) {
	t.Run("preparing", func(t *testing.T) {
		status := mapLocalSTTStatus(localSTTStatus{
			Model: "mlx-community/whisper-large-v3-mlx", PrepareState: "preparing",
			PrepareAttempt: 2, PrepareMaxAttempts: 3,
		})
		if status.State != "preparing" || status.CanRetry || status.Attempt != 2 {
			t.Fatalf("status=%#v", status)
		}
	})

	t.Run("ready wins over stale prepare state", func(t *testing.T) {
		status := mapLocalSTTStatus(localSTTStatus{WeightsReady: true, PrepareState: "preparing"})
		if status.State != "ready" || status.CanRetry {
			t.Fatalf("status=%#v", status)
		}
	})

	t.Run("failed is retryable", func(t *testing.T) {
		message := "network timeout"
		status := mapLocalSTTStatus(localSTTStatus{PrepareState: "failed", PrepareError: &message})
		if status.State != "failed" || !status.CanRetry || status.Message == "" {
			t.Fatalf("status=%#v", status)
		}
	})

	t.Run("timed out worker cannot queue a duplicate retry", func(t *testing.T) {
		message := "本地語音模型準備超過 30 分鐘，請重試"
		status := mapLocalSTTStatus(localSTTStatus{
			PrepareState: "failed", PrepareError: &message, WorkerBusy: true,
		})
		if status.CanRetry || !strings.Contains(status.Message, "完成後便可重試") {
			t.Fatalf("status=%#v", status)
		}
	})
}

func TestSTTStatusAndPrepareProxy(t *testing.T) {
	prepareCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/readyz":
			writeJSON(writer, http.StatusOK, map[string]any{
				"model": "test-model", "weights_ready": false, "prepare_state": "preparing",
				"prepare_attempt": 1, "prepare_max_attempts": 3,
			})
		case "/prepare":
			prepareCalls++
			writeJSON(writer, http.StatusAccepted, map[string]string{"prepare_state": "preparing"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer upstream.Close()

	server := &Server{asrProvider: "local", localSTTURL: upstream.URL, sttClient: upstream.Client()}
	statusRecorder := httptest.NewRecorder()
	server.SttStatusApiSttStatusGet(statusRecorder, httptest.NewRequest(http.MethodGet, "/api/stt/status", nil))
	if statusRecorder.Code != http.StatusOK || !strings.Contains(statusRecorder.Body.String(), `"state":"preparing"`) {
		t.Fatalf("status code=%d body=%s", statusRecorder.Code, statusRecorder.Body.String())
	}

	prepareRecorder := httptest.NewRecorder()
	server.SttPrepareApiSttPreparePost(prepareRecorder, httptest.NewRequest(http.MethodPost, "/api/stt/prepare", nil))
	if prepareRecorder.Code != http.StatusAccepted || prepareCalls != 1 {
		t.Fatalf("prepare code=%d calls=%d body=%s", prepareRecorder.Code, prepareCalls, prepareRecorder.Body.String())
	}
}
