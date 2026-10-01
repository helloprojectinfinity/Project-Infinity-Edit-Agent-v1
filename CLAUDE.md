# Rushes

本地優先的對話式影片剪接 Agent。後端是 Go 1.26 + Eino + chi + modernc SQLite，前端是 React 19 + Vite 7。

- 面向使用者的文案、錯誤與 Agent 台詞一律使用香港繁體中文書面語；技術名詞（Agent / Timeline / WorldState / Harness / Reducer / Frame / Beat / Clip / Track / BGM 等）保留英文。
- 根目錄 `.env` 是所有本地手動、開發和 E2E 流程的統一設定來源；已明確 export 的同名變數優先，任何日誌和測試輸出都不得洩漏密鑰。
- **供使用者手測的本地環境必須載入倉庫根 `.env`**：無論透過 dev、test、E2E、tmux 還是手動指令啟動，都要確保 API 和 worker 在啟動前按 `scripts/dev_all.sh` 與 `go/internal/config.LoadDotEnv` 的語意載入 `.env`（已明確 export 的變數優先，`.env` 只補未設定項）。不要把後端密鑰注入 Web；確定性的自動化測試若必須停用真實 provider，應明確覆寫／清空並說明。交付前檢查執行中 API／worker 已繼承所需變數並完成健康檢查，但絕不輸出密鑰值。

## 常用命令

```bash
make dev                              # Go API + worker + web
make contracts                        # OpenAPI 與兩套 SSE golden 對拍
make test                             # cd go && go test -race ./...
make coverage                         # 手寫 Go 核心覆蓋率 >= 90%
make lint                             # go vet + golangci-lint/depguard
make web                              # typecheck + vitest + build
make e2e                              # Playwright 指向 Go 後端
```

真實 provider 測試帶 `integration` build tag，預設必須跳過；只有設定真實密鑰並明確要求時才用 `RUSHES_REQUIRE_LIVE_MODELS=1` 強制執行。

## 寫入路徑與分層

- `go/internal/reducer` 是唯一業務寫入路徑；事件、物化狀態和 ResultRows 必須在同一個 immediate transaction 內提交。
- `go/internal/contracts` 的 EventRegistry 是事件事實源；新增事件必須同時實作驗證、Reducer apply、SSE routing 與測試。
- `go/internal/tools` 的 registry 是工具事實源；LLM 工具必須經過 precondition、PolicyGate 欄位檢查與 Agent 統一執行入口。
- `go/internal/worker` 只透過 claim／heartbeat／retry 協定處理 job，終態繼續走 Reducer。
- `go/.golangci.yml` 的 depguard 是依賴方向權威；不要透過中介套件或相對匯入繞過。
- `apps/web/openapi.json` 是凍結 HTTP 契約；修改後執行 `make contracts`，生成物必須零 diff。
