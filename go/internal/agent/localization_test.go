package agent

import "testing"

func TestTraditionalBoundaryIntentsPreserveNegation(t *testing.T) {
	if !hasTimelineMutationIntent("剪接影片，調整時間線") ||
		!hasBeatEditIntent("按照音樂拍點剪接") ||
		!hasExplicitPreviewIntent("生成預覽") ||
		!hasUserFinalExportIntent("匯出最終影片") {
		t.Fatal("繁體指令未識別")
	}
	for _, text := range []string{"不要匯出，只要預覽", "唔好匯出，只要預覽"} {
		positive := withoutNegatedBoundaryActions(text)
		if hasUserFinalExportIntent(positive) || !hasExplicitPreviewIntent(positive) {
			t.Fatalf("否定指令失效: %q -> %q", text, positive)
		}
	}
	if !hasTimelineMutationIntent("剪辑视频，调整时间线") {
		t.Fatal("舊簡體指令不再相容")
	}
}
