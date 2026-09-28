package agent

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// These text helpers decide only Harness-owned preview, export and analysis
// boundaries. They never select, add or remove provider action schemas.
func latestUserIntentText(messages []*schema.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message == nil || message.Role != schema.User {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content != "" && !isDecisionContinuationMessage(content) {
			return strings.ToLower(content)
		}
	}
	return ""
}

func isDecisionContinuationMessage(content string) bool {
	return strings.Contains(content, "这是同一条任务的继续，不是新的请求。")
}

var negatableBoundaryActions = []string{
	"生成预览", "可分享预览", "preview.generate", "最终成片", "最终视频",
	"移动片段", "剪辑", "剪掉", "裁剪", "裁到", "分割", "淡入", "淡出",
	"编辑", "修改", "调整", "插入", "添加", "替换", "渲染", "预览", "导出",
	"下载", "mp4", "质检", "黑帧", "静帧", "静音", "响度", "解码",
}

// Only normalize a temporary copy for boundary matching, never stored messages,
// filenames, subtitles or tool arguments. Keep existing simplified rules valid.
var boundaryLanguageAliases = strings.NewReplacer(
	"唔需要", "不需要", "唔好", "不要", "毋須", "无需", "無需", "无需",
	"暫不", "暂不", "別再", "别再", "請別", "请别",
	"然後", "然后", "隨後", "随后", "接著", "接着",
	"改為", "改为", "轉而", "转而", "不過", "不过",
	"匯出", "导出", "導出", "导出", "下載", "下载", "最終", "最终",
	"影片", "视频", "視頻", "视频", "剪接", "剪辑", "剪輯", "剪辑",
	"預覽", "预览", "離線畫質", "离线画质", "編輯", "编辑",
	"調整", "调整", "移動", "移动", "替換", "替换", "刪除", "删除",
	"時間線", "时间线", "軌道", "轨道", "修復", "修复",
	"卡點", "卡点", "踩點", "踩点", "拍點", "拍点", "節拍", "节拍",
	"質檢", "质检", "黑幀", "黑帧", "靜幀", "静帧", "靜音", "静音",
	"響度", "响度", "解碼", "解码", "視覺", "视觉", "畫面", "画面",
	"裁邊", "裁边", "構圖", "构图", "調色", "调色", "顏色", "颜色",
	"轉場", "转场", "遮擋", "遮挡", "黑邊", "黑边",
	"直向", "纵向", "縱向", "纵向", "豎屏", "竖屏", "豎版", "竖版",
	"橫屏", "横屏", "橫版", "横版", "橫向", "横向",
	"就緒", "就绪", "處理", "处理", "已準備好", "已准备好",
	"已經", "已经", "無法", "无法", "執行失敗", "执行失败",
	"還需要", "还需要", "用戶", "用户", "未達到", "未达到",
	"終驗未通過", "终验未通过", "未通過", "未通过",
)

func withoutNegatedBoundaryActions(text string) string {
	text = boundaryLanguageAliases.Replace(text)
	var positive strings.Builder
	for index := 0; index < len(text); {
		matched := ""
		for _, action := range negatableBoundaryActions {
			if strings.HasPrefix(text[index:], action) && len(action) > len(matched) {
				matched = action
			}
		}
		if matched != "" && boundaryActionIsNegated(text, index) {
			index += len(matched)
			continue
		}
		positive.WriteByte(text[index])
		index++
	}
	return positive.String()
}

func boundaryActionIsNegated(text string, actionStart int) bool {
	clauseStart := 0
	for index, character := range text[:actionStart] {
		switch character {
		case '，', ',', '；', ';', '。', '.', '！', '!', '\n', '\r':
			clauseStart = index + len(string(character))
		}
	}
	prefix := text[clauseStart:actionStart]
	lastNegative := lastBoundaryKeywordIndex(prefix,
		"不要", "不需要", "无需", "不必", "不得", "不能", "禁止", "暂不", "别再", "请别",
	)
	trimmed := strings.TrimSpace(prefix)
	if strings.HasPrefix(trimmed, "别") && lastNegative < 0 {
		lastNegative = 0
	}
	if strings.HasSuffix(trimmed, "别") || strings.HasSuffix(trimmed, "不") {
		lastNegative = len(prefix)
	}
	if lastNegative < 0 {
		return false
	}
	lastPivot := lastBoundaryKeywordIndex(prefix,
		"而是", "只需", "只要", "然后", "随后", "接着", "改为", "转而", "即可", "直接", "但是", "不过", "但",
	)
	return lastPivot <= lastNegative || !containsBoundaryKeyword(
		prefix[lastNegative:lastPivot], negatableBoundaryActions...,
	)
}

func lastBoundaryKeywordIndex(text string, keywords ...string) int {
	latest := -1
	for _, keyword := range keywords {
		if index := strings.LastIndex(text, keyword); index > latest {
			latest = index
		}
	}
	return latest
}

func hasTimelineMutationIntent(text string) bool {
	if containsBoundaryKeyword(text,
		"剪辑", "剪掉", "裁剪", "裁到", "分割", "移动片段", "淡入", "淡出",
		"编辑", "修改", "调整", "修复", "clip", "patch",
	) {
		return true
	}
	return containsBoundaryKeyword(text, "时间线", "轨道", "字幕") &&
		containsBoundaryKeyword(text, "插入", "添加", "替换", "删除", "修改", "调整")
}

func hasBeatEditIntent(text string) bool {
	return containsBoundaryKeyword(text, "卡点", "踩点", "拍点", "节拍", "bpm", "bgm", "beat")
}

func hasPreviewCheckIntent(text string) bool {
	return containsBoundaryKeyword(text,
		"质检", "黑帧", "静帧", "静音", "响度", "解码", "render_preview 已完成", "preview_",
	)
}

func hasExplicitPreviewIntent(text string) bool {
	return containsBoundaryKeyword(text, "预览", "离线画质", "preview.generate")
}

func hasPreviewOnlyIntent(text string) bool {
	return (hasExplicitPreviewIntent(text) || hasPreviewCheckIntent(text)) &&
		!hasTimelineMutationIntent(text) && !hasUserFinalExportOnlyIntent(text)
}

func hasUserFinalExportIntent(text string) bool {
	return containsBoundaryKeyword(text, "导出", "下载", "最终成片", "最终视频", "渲染成片", "mp4")
}

func hasUserFinalExportOnlyIntent(text string) bool {
	return hasUserFinalExportIntent(text) && !hasTimelineMutationIntent(text) &&
		!hasExplicitPreviewIntent(text) && !containsBoundaryKeyword(text, "预览质检")
}

func containsBoundaryKeyword(text string, keywords ...string) bool {
	text = boundaryLanguageAliases.Replace(text)
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
