import { ApiError } from "../../auth";
import type { DraftTimelineResponse } from "../../api/client";

export type TimelinePatchPartialFailure = {
  appliedCount: number;
  failedIndex: number;
  latest: DraftTimelineResponse;
};

export function timelinePatchPartialFailure(
  error: unknown
): TimelinePatchPartialFailure | null {
  if (!(error instanceof ApiError) || !error.payload || typeof error.payload !== "object") {
    return null;
  }
  const detail = Reflect.get(error.payload, "detail");
  if (!detail || typeof detail !== "object") {
    return null;
  }
  const appliedCount = Reflect.get(detail, "applied_count");
  const failedIndex = Reflect.get(detail, "failed_index");
  const latest = Reflect.get(detail, "latest");
  if (
    !Number.isInteger(appliedCount) ||
    appliedCount < 0 ||
    !Number.isInteger(failedIndex) ||
    failedIndex < 0 ||
    !latest ||
    typeof latest !== "object" ||
    typeof Reflect.get(latest, "timeline_version") !== "number" ||
    !Reflect.get(latest, "timeline") ||
    typeof Reflect.get(latest, "timeline") !== "object"
  ) {
    return null;
  }
  return {
    appliedCount,
    failedIndex,
    latest: latest as DraftTimelineResponse
  };
}

// 从后端错误里提取可读 reason：优先取 ApiError.payload.detail.reason，否则回退到
// error.message。时间线写路径与会话（清空/取消任务/回退）共用这一层，故抽成叶子模块，
// 避免 DraftEditorView 与 ConsolePanel 之间产生循环依赖。
export function timelinePatchErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.payload && typeof error.payload === "object") {
    const detail = Reflect.get(error.payload, "detail");
    if (detail && typeof detail === "object") {
      const reason = Reflect.get(detail, "reason");
      if (typeof reason === "string" && reason.trim()) {
        if (reason === "timeline_locked_by_agent") {
          return "Agent 正在編輯，請等待本輪結束後再修改時間線。";
        }
        return reason;
      }
    }
  }
  return error instanceof Error ? error.message : "時間線修改失敗";
}

export function conversationClearErrorMessage(error: unknown): string {
  const reason = timelinePatchErrorMessage(error);
  if (reason === "turn_active") {
    return "當前任務仍在執行，請先停止或等待本輪結束後再清空對話。";
  }
  return error instanceof ApiError && error.status === 409 ? "當前任務仍在執行，暫時不能清空對話。" : reason;
}

export function jobCancelErrorMessage(error: unknown): string {
  const reason = timelinePatchErrorMessage(error);
  if (reason === "job_not_cancellable" || (error instanceof ApiError && error.status === 409)) {
    return "任務狀態已變化，無法取消；已刷新當前狀態。";
  }
  return `取消任務失敗：${reason}`;
}

export function resendErrorMessage(error: unknown): string {
  const reason = timelinePatchErrorMessage(error);
  if (reason === "resend_cancellation_timeout" || reason === "resend_in_progress") {
    return "當前任務尚未安全停止，請稍後重試。";
  }
  if (reason === "resend_checkpoint_unavailable") {
    return "這條訊息太早了，已無法回到它之前的狀態。";
  }
  if (reason === "resend_message_not_found") {
    return "這條訊息已不存在，請刷新後重試。";
  }
  if (reason === "resend_message_not_editable") {
    return "這條訊息已被新的編輯覆蓋，請刷新後重試。";
  }
  if (reason === "resend_job_state_changed") {
    return "任務狀態剛剛發生變化，請稍後重試。";
  }
  if (reason === "resend_idempotency_key_reused") {
    return "這次重發的參數已變化，請重新操作。";
  }
  if (reason === "turn_queue_closed") {
    return "剪接任務佇列已停止，請重啟本地服務後再重發。";
  }
  if (reason === "empty_message") {
    return "訊息內容不能為空。";
  }
  if (reason === "version_conflict" || (error instanceof ApiError && error.status === 409)) {
    return "草稿剛剛發生了變化，請刷新後重試。";
  }
  return `編輯重發失敗：${reason}`;
}
