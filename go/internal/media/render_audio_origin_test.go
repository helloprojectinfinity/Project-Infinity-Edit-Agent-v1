package media

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nanzhi84/Rushes/go/internal/storage"
	"github.com/nanzhi84/Rushes/go/internal/timeline"
)

// TestRenderAudioMixResetsTimelineOrigin 覆盖 v139 最终导出报
// "Application provided invalid, non monotonically increasing dts to muxer in stream 1"
// 的回归。
//
// 成因：audioFilter 用 adelay 把每个 clip 的 PTS 平移到它在时间线上的位置，而
// mixAudioFilter 混音后没有用 asetpts 归零。当输入是 -ss 越过素材起点的取样
// （PTS 不从 0 开始）且 adelay 较大时，FFmpeg 9.0.2 的 adelay 会产出
// PTS=AV_NOPTS_VALUE（INT64_MAX）的帧；adelay 单独存在时不会，但一旦喂给
// amix，amix 会把坏帧原样交给 AAC encoder，muxer 报上面的错误，导出失败。
//
// 三个条件必须同时成立才会触发，缺一不可：
//  1. 输入 -ss 越过素材起点（PTS 起点非 0）；
//  2. adelay 较大（真实项目里是 87.5s~218.7s 这类"排到成片后面"的位置）；
//  3. 音轨有多个 clip 走 amix 多输入分支。
//
// 混音链末尾补 asetpts=PTS-STARTPTS 即可把每条音轨的起点归零。
func TestRenderAudioMixResetsTimelineOrigin(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "source.mp4")
	if _, err := RunCommand(t.Context(), "ffmpeg",
		"-y", "-f", "lavfi", "-i", "testsrc2=s=160x90:r=30:d=20",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=20",
		"-map", "0:v", "-map", "1:a",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", source,
	); err != nil {
		t.Skipf("无法生成测试素材: %v", err)
	}

	database, err := storage.Open(t.Context(), tempDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.Paths.Temporary = tempDir
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(t.Context(), `
		INSERT INTO assets(asset_id,storage_mode,reference_path,kind,source,filename,hash,mtime,size,ingest_status,usable)
		VALUES(?,?,?,?,?,?,?,?,?,'ready',1)`,
		"asset_source", "reference", source, "video", "local_path", "source.mp4", "asset_source",
		info.ModTime().UnixNano(), info.Size()); err != nil {
		t.Fatal(err)
	}

	const fps = 30
	document := timeline.Document{
		DraftID:        "draft_audio_origin",
		FPS:            fps,
		DurationFrames: 220 * fps,
		Tracks: []timeline.Track{
			{TrackID: "visual_base"},
			{TrackID: "original_audio", Muted: true},
			{TrackID: "voiceover"},
		},
	}
	// 主视觉铺满全片，避免 concat 提前结束整条滤镜图。
	document.Tracks[0].Clips = append(document.Tracks[0].Clips, timeline.Clip{
		TimelineClipID:     "base_001",
		TrackID:            "visual_base",
		AssetID:            "asset_source",
		AssetKind:          "video",
		Role:               "b_roll",
		TimelineStartFrame: 0,
		TimelineEndFrame:   220 * fps,
		SourceStartFrame:   0,
		SourceEndFrame:     220 * fps,
		PlaybackRate:       1,
	})
	// 三个 clip 分别排在 87.5s / 140.3s / 210.6s => adelay 远超阈值；
	// 取样起点 5s / 8s / 12s 全部非 0。取样区间在 20s 素材内，不依赖"越过 EOF"。
	voiceover := []struct {
		id        string
		startSec  float64
		lengthSec float64
	}{
		{"voice_01", 87.5, 2.566667},
		{"voice_02", 140.333333, 5.8},
		{"voice_03", 210.633333, 8.066667},
	}
	for _, voice := range voiceover {
		document.Tracks[2].Clips = append(document.Tracks[2].Clips, timeline.Clip{
			TimelineClipID:     voice.id,
			TrackID:            "voiceover",
			AssetID:            "asset_source",
			AssetKind:          "video",
			Role:               "voiceover",
			TimelineStartFrame: int(voice.startSec * fps),
			TimelineEndFrame:   int((voice.startSec + voice.lengthSec) * fps),
			SourceStartFrame:   int((5 + voice.lengthSec) * fps),
			SourceEndFrame:     int((5 + 2*voice.lengthSec) * fps),
			PlaybackRate:       1,
		})
	}

	result, err := RenderTimeline(t.Context(), database, document, PreviewProfile, nil)
	if err != nil {
		t.Fatalf("adelay 较大的多条音轨混音应能正常导出，实际失败: %v", err)
	}
	if want := 220.0; result.DurationSec != want {
		t.Errorf("导出时长 = %v，期望 %v", result.DurationSec, want)
	}
}
