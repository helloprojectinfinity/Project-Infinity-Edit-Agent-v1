#!/usr/bin/env bash
set -euo pipefail

# 一条命令拉起 Go API、Go worker 与 Vite。根目录 .env 会被读取，但已 export
# 的同名变量始终优先；端口冲突直接失败，避免前端误连到别的本地服务。
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

load_dotenv() {
  local env_file="$1"
  [[ -f "$env_file" ]] || return 0
  local line key value
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    line="${line#"${line%%[![:space:]]*}"}"
    case "$line" in '' | '#'* ) continue ;; esac
    line="${line#export }"
    case "$line" in *=*) ;; *) continue ;; esac
    key="${line%%=*}"
    value="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"
    case "$key" in '' | *[!A-Za-z0-9_]*) continue ;; esac
    [[ -n "${!key+x}" ]] && continue
    case "$value" in
      \"*\") value="${value#\"}"; value="${value%\"}" ;;
      \'*\') value="${value#\'}"; value="${value%\'}" ;;
    esac
    export "$key=$value"
  done <"$env_file"
}

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
    return
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltn "sport = :$port" | tail -n +2 | grep -q .
    return
  fi
  return 1
}

load_dotenv "$ROOT/.env"

# 优先使用 ffmpeg-full（含 libass／freetype／harfbuzz 等字幕与文字渲染依赖），
# 解决 brew keg-only 不写入 PATH 的问题；找不到时退回系统 ffmpeg 并给出警告。
FFMPEG_FULL_BIN="$(brew --prefix ffmpeg-full 2>/dev/null || true)/bin"
if [[ -x "$FFMPEG_FULL_BIN/ffmpeg" && -x "$FFMPEG_FULL_BIN/ffprobe" ]]; then
  # 完整读取 filter 列表再匹配：避免 `grep -q` 提早关闭管道让 ffmpeg 收到 SIGPIPE。
  # 用临时文件而不是命令替换，命令替换会把 ffmpeg 的退出码吞掉，
  # 那样 `|| true` 之后的 if 判断永远不会成立，坏掉的 ffmpeg 反而能通过检查。
  filters_file="$(mktemp)"
  if ! "$FFMPEG_FULL_BIN/ffmpeg" -hide_banner -filters >"$filters_file" 2>/dev/null; then
    rm -f "$filters_file"
    printf '\033[31m错误：%s 无法列出 filter；请重新执行 brew install ffmpeg-full 后再启动。\033[0m\n' "$FFMPEG_FULL_BIN/ffmpeg" >&2
    exit 1
  fi
  if ! grep -qE ' [.]+ subtitles ' "$filters_file"; then
    rm -f "$filters_file"
    printf '\033[31m错误：%s 缺少 subtitles filter；请重新执行 brew install ffmpeg-full 后再启动。\033[0m\n' "$FFMPEG_FULL_BIN/ffmpeg" >&2
    exit 1
  fi
  rm -f "$filters_file"
  export PATH="$FFMPEG_FULL_BIN:$PATH"
else
  printf '\033[33m警告：未检测到 ffmpeg-full；subtitles 字幕渲染可能不可用。请执行 brew install ffmpeg-full。\033[0m\n' >&2
fi

persist_generated_token() {
  local env_file="$1"
  local token="$2"
  local temporary old_umask
  old_umask="$(umask)"
  umask 077
  touch "$env_file"
  temporary="$(mktemp "${env_file}.tmp.XXXXXX")"
  awk -v token="$token" '
    BEGIN { replaced = 0 }
    {
      candidate = $0
      sub(/^[[:space:]]*export[[:space:]]+/, "", candidate)
      if (candidate ~ /^[[:space:]]*RUSHES_API_TOKEN[[:space:]]*=/) {
        if (!replaced) {
          print "RUSHES_API_TOKEN=" token
          replaced = 1
        }
        next
      }
      print
    }
    END {
      if (!replaced) print "RUSHES_API_TOKEN=" token
    }
  ' "$env_file" >"$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$env_file"
  umask "$old_umask"
}

API_PORT="${RUSHES_API_PORT:-8010}"
WEB_PORT="${RUSHES_WEB_PORT:-8011}"
# worker 度量端口取 API_PORT+2，避开 WEB_PORT(默认 API_PORT+1) 撞车（#95 H3 P1）。
WORKER_METRICS_PORT="${RUSHES_WORKER_METRICS_PORT:-$((API_PORT + 2))}"
LOCAL_STT_PORT="${RUSHES_LOCAL_STT_PORT:-8013}"
WORKSPACE="${RUSHES_WORKSPACE_PATH:-$ROOT/.rushes}"
case "$WORKSPACE" in
  /*) ;;
  *) WORKSPACE="$ROOT/$WORKSPACE" ;;
esac
if [[ -n "${RUSHES_API_TOKEN:-}" ]]; then
  TOKEN="$RUSHES_API_TOKEN"
else
  TOKEN="$(openssl rand -hex 32)"
  persist_generated_token "$ROOT/.env" "$TOKEN"
  export RUSHES_API_TOKEN="$TOKEN"
  printf '\033[32m已生成本地启动 token 并安全保存到 .env；后续启动将自动复用。\033[0m\n'
fi
BIN_DIR="$WORKSPACE/bin"

CHAT_PROVIDER="$(printf '%s' "${RUSHES_CHAT_PROVIDER:-dashscope}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
if [[ "$CHAT_PROVIDER" == "dashscope" && -z "${RUSHES_DASHSCOPE_API_KEY:-}" ]]; then
  printf '\033[33m警告：未配置 RUSHES_DASHSCOPE_API_KEY；本地链路可运行，但 Agent 会使用无模型降级回复。\033[0m\n' >&2
elif [[ "$CHAT_PROVIDER" == "ark" && -z "${RUSHES_ARK_API_KEY:-}" && ( -z "${RUSHES_ARK_ACCESS_KEY:-}" || -z "${RUSHES_ARK_SECRET_KEY:-}" ) ]]; then
  printf '\033[33m警告：RUSHES_CHAT_PROVIDER=ark 但未配置 RUSHES_ARK_API_KEY（或 AK/SK）；API 与 worker 会在启动期报错。\033[0m\n' >&2
elif [[ "$CHAT_PROVIDER" == "openrouter" && -z "${RUSHES_OPENROUTER_API_KEY:-}" ]]; then
  printf '\033[33m警告：RUSHES_CHAT_PROVIDER=openrouter 但未配置 RUSHES_OPENROUTER_API_KEY；API 与 worker 会在启动期报错。\033[0m\n' >&2
fi

ASR_PROVIDER="$(printf '%s' "${RUSHES_ASR_PROVIDER:-local}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
# 只有 dev_all 负责启停本地 STT 时才预留端口；自管 URL 或选 dashscope 时让用户自己管。
manage_local_stt=0
case "$ASR_PROVIDER" in
  dashscope) manage_local_stt=0 ;;
  local)
    if [[ -z "${RUSHES_LOCAL_STT_URL:-}" ]]; then
      manage_local_stt=1
    fi
    ;;
  *)
    echo "错误：RUSHES_ASR_PROVIDER=$ASR_PROVIDER 非法，合法值为 local 或 dashscope。" >&2
    exit 1
    ;;
esac

ports_to_check=("$API_PORT" "$WEB_PORT" "$WORKER_METRICS_PORT")
if [[ "$manage_local_stt" == 1 ]]; then
  ports_to_check+=("$LOCAL_STT_PORT")
fi
for port in "${ports_to_check[@]}"; do
  if port_in_use "$port"; then
    echo "错误：端口 $port 已被占用。请设置 RUSHES_API_PORT / RUSHES_WEB_PORT / RUSHES_LOCAL_STT_PORT 后重试。" >&2
    exit 1
  fi
done

mkdir -p "$BIN_DIR"
(
  cd "$ROOT/go"
  go build -o "$BIN_DIR/rushes-api" ./cmd/api
  go build -o "$BIN_DIR/rushes-worker" ./cmd/worker
)

# 子进程 PID 用命名变量保存：macOS 系统 Bash 3.2 不支持 ${arr[-1]} 之类的负索引。
# 四个变量必须先全部初始化：cleanup 会在任何一个子进程启动之前就可能被 trap
# 触发，set -u 下未定义变量会让清理本身报错，反而盖掉真正的失败原因。
stt_pid=""
api_pid=""
worker_pid=""
web_pid=""
cleanup() {
  trap - EXIT INT TERM
  [[ -n "$stt_pid" ]] && kill "$stt_pid" 2>/dev/null || true
  [[ -n "$api_pid" ]] && kill "$api_pid" 2>/dev/null || true
  [[ -n "$worker_pid" ]] && kill "$worker_pid" 2>/dev/null || true
  [[ -n "$web_pid" ]] && kill "$web_pid" 2>/dev/null || true
  [[ -n "$stt_pid" ]] && wait "$stt_pid" 2>/dev/null || true
  [[ -n "$api_pid" ]] && wait "$api_pid" 2>/dev/null || true
  [[ -n "$worker_pid" ]] && wait "$worker_pid" 2>/dev/null || true
  [[ -n "$web_pid" ]] && wait "$web_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

if [[ "$manage_local_stt" == 1 ]]; then
  LOCAL_STT_DIR="$ROOT/services/local-stt"
  LOCAL_STT_VENV="$WORKSPACE/.local-stt-venv"
  if [[ ! -x "$LOCAL_STT_VENV/bin/python" ]]; then
    printf '\033[33m首次启动：正在准备本地 STT Python 环境（约数秒至数分钟）\033[0m\n'
    if command -v uv >/dev/null 2>&1; then
      (cd "$LOCAL_STT_DIR" && uv venv --python 3.12 "$LOCAL_STT_VENV" && uv pip install --python "$LOCAL_STT_VENV/bin/python" -e .) || {
        echo "错误：本地 STT Python 环境准备失败。" >&2
        exit 1
      }
    else
      (cd "$LOCAL_STT_DIR" && python3 -m venv "$LOCAL_STT_VENV" && "$LOCAL_STT_VENV/bin/pip" install -e .) || {
        echo "错误：本地 STT Python 环境准备失败；请先安装 uv 或 Python 3.12+。" >&2
        exit 1
      }
    fi
  fi

  RUSHES_LOCAL_STT_URL="http://127.0.0.1:$LOCAL_STT_PORT" \
    RUSHES_LOCAL_STT_PORT="$LOCAL_STT_PORT" \
    RUSHES_LOCAL_STT_HOST=127.0.0.1 \
    "$LOCAL_STT_VENV/bin/python" -m rushes_stt &
  stt_pid="$!"

  stt_ready=0
  for _ in $(seq 1 60); do
    if curl --silent --fail "http://127.0.0.1:$LOCAL_STT_PORT/healthz" >/dev/null 2>&1; then
      stt_ready=1
      break
    fi
    if ! kill -0 "$stt_pid" 2>/dev/null; then
      echo "错误：本地 STT 服务启动失败。" >&2
      exit 1
    fi
    sleep 0.25
  done
  if [[ "$stt_ready" != 1 ]]; then
    echo "错误：本地 STT 服务在 15 秒内未就绪。" >&2
    exit 1
  fi
fi

RUSHES_WORKSPACE_PATH="$WORKSPACE" RUSHES_API_TOKEN="$TOKEN" RUSHES_API_PORT="$API_PORT" \
  "$BIN_DIR/rushes-api" -env-file "$ROOT/.env" -workspace "$WORKSPACE" -port "$API_PORT" &
api_pid="$!"

ready=0
for _ in $(seq 1 120); do
  if curl --silent --fail "http://127.0.0.1:$API_PORT/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$api_pid" 2>/dev/null; then
    echo "错误：Rushes API 启动失败。" >&2
    exit 1
  fi
  sleep 0.25
done
if [[ "$ready" != 1 ]]; then
  echo "错误：Rushes API 在 30 秒内未就绪。" >&2
  exit 1
fi

RUSHES_WORKSPACE_PATH="$WORKSPACE" RUSHES_WORKER_METRICS_ADDR="127.0.0.1:$WORKER_METRICS_PORT" \
  "$BIN_DIR/rushes-worker" -env-file "$ROOT/.env" -workspace "$WORKSPACE" &
worker_pid="$!"

env -u RUSHES_DASHSCOPE_API_KEY -u RUSHES_API_TOKEN \
  RUSHES_WEB_PROXY_TARGET="http://127.0.0.1:$API_PORT" \
  npx -y pnpm@10.13.1 --dir "$ROOT/apps/web" dev --host 127.0.0.1 --port "$WEB_PORT" --strictPort &
web_pid="$!"

echo
echo "════════════════════════════════════════════════════"
echo "  Rushes Go 全栈已启动："
echo "  日常访问：http://127.0.0.1:$WEB_PORT"
echo "  首次登录：http://127.0.0.1:$WEB_PORT/#t=$TOKEN"
echo "  API :$API_PORT · workspace: $WORKSPACE · Ctrl+C 全停"
if [[ "$manage_local_stt" == 1 ]]; then
  # 注意：Bash 3.2（macOS 系统默认）解析 $VAR紧贴中文全角括号时会丢字符，
  # 把变量名误读为包含 CJK 字节并报「unbound variable」。必须用 ${VAR} 形式定界。
  echo "  STT :本地 Whisper http://127.0.0.1:${LOCAL_STT_PORT}（首次请求时下载并加载模型）"
elif [[ "$ASR_PROVIDER" == "dashscope" ]]; then
  echo "  STT :DashScope 云端（已在 cmd/api/main.go 内注入 RUSHES_DASHSCOPE_API_KEY 时装配）"
else
  echo "  STT :使用自管服务 ${RUSHES_LOCAL_STT_URL}（dev_all 不启停本机服务）"
fi
echo "  日志 :${WORKSPACE}/logs/{api,worker}.log（JSON 结构化，按大小轮转，同时镜像到本终端）"
echo "  度量 :API http://127.0.0.1:${API_PORT}/debug/metrics · worker http://127.0.0.1:${WORKER_METRICS_PORT}/debug/metrics"
echo "════════════════════════════════════════════════════"

while true; do
  for pid in "$stt_pid" "$api_pid" "$worker_pid" "$web_pid"; do
    [[ -z "$pid" ]] && continue
    if ! kill -0 "$pid" 2>/dev/null; then
      status=0
      wait "$pid" 2>/dev/null || status=$?
      exit "$status"
    fi
  done
  sleep 1
done
