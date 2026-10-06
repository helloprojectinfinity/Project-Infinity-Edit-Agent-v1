package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type localSTTStatus struct {
	Model              string  `json:"model"`
	WeightsReady       bool    `json:"weights_ready"`
	PrepareState       string  `json:"prepare_state"`
	PrepareError       *string `json:"prepare_error"`
	PrepareAttempt     int     `json:"prepare_attempt"`
	PrepareMaxAttempts int     `json:"prepare_max_attempts"`
	WorkerBusy         bool    `json:"worker_busy"`
}

func (server *Server) SttStatusApiSttStatusGet(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, server.sttStatus(request))
}

func (server *Server) SttPrepareApiSttPreparePost(writer http.ResponseWriter, request *http.Request) {
	if server.asrProvider != "local" {
		writeJSON(writer, http.StatusAccepted, server.cloudSTTStatus())
		return
	}
	if server.localSTTURL == "" {
		writeJSON(writer, http.StatusAccepted, unavailableSTTStatus("未設定本地語音服務地址"))
		return
	}

	downstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, server.localSTTURL+"/prepare", nil)
	if err == nil {
		var response *http.Response
		response, err = server.sttClient.Do(downstream)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
			_ = response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				err = fmt.Errorf("本地語音服務回傳 HTTP %d", response.StatusCode)
			}
		}
	}
	if err != nil {
		writeJSON(writer, http.StatusAccepted, unavailableSTTStatus("無法啟動本地語音模型準備，請確認服務正在運行"))
		return
	}
	writeJSON(writer, http.StatusAccepted, server.sttStatus(request))
}

func (server *Server) sttStatus(request *http.Request) SttStatusResponse {
	if server.asrProvider != "local" {
		return server.cloudSTTStatus()
	}
	if server.localSTTURL == "" {
		return unavailableSTTStatus("未設定本地語音服務地址")
	}

	downstream, err := http.NewRequestWithContext(request.Context(), http.MethodGet, server.localSTTURL+"/readyz", nil)
	if err != nil {
		return unavailableSTTStatus("無法讀取本地語音服務狀態")
	}
	response, err := server.sttClient.Do(downstream)
	if err != nil {
		return unavailableSTTStatus("本地語音服務未能連線")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return unavailableSTTStatus(fmt.Sprintf("本地語音服務回傳 HTTP %d", response.StatusCode))
	}
	var status localSTTStatus
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&status); err != nil {
		return unavailableSTTStatus("本地語音服務狀態格式無效")
	}
	return mapLocalSTTStatus(status)
}

func (server *Server) cloudSTTStatus() SttStatusResponse {
	return SttStatusResponse{
		Mode: "cloud", State: "ready", Model: "DashScope ASR",
		Message: "語音辨識使用 DashScope 雲端服務", CanRetry: false,
		Attempt: 0, MaxAttempts: 0,
	}
}

func mapLocalSTTStatus(status localSTTStatus) SttStatusResponse {
	state := strings.TrimSpace(status.PrepareState)
	if status.WeightsReady {
		state = "ready"
	}
	message := "本地語音模型尚未開始準備"
	canRetry := state == "idle" || state == "failed"
	switch state {
	case "preparing":
		message = "正在下載及暖機本地語音模型；其他剪接功能可繼續使用"
	case "ready":
		message = "本地語音模型已就緒"
		canRetry = false
	case "failed":
		message = "本地語音模型準備失敗"
		if status.PrepareError != nil && strings.TrimSpace(*status.PrepareError) != "" {
			message += "：" + strings.TrimSpace(*status.PrepareError)
		}
		if status.WorkerBusy {
			message += "；目前的模型程序仍在停止，完成後便可重試"
			canRetry = false
		}
	default:
		state = "idle"
	}
	return SttStatusResponse{
		Mode: "local", State: state,
		Model: status.Model, Message: message, CanRetry: canRetry,
		Attempt: status.PrepareAttempt, MaxAttempts: status.PrepareMaxAttempts,
	}
}

func unavailableSTTStatus(message string) SttStatusResponse {
	return SttStatusResponse{
		Mode: "local", State: "unavailable", Model: "",
		Message: message, CanRetry: true, Attempt: 0, MaxAttempts: 3,
	}
}
