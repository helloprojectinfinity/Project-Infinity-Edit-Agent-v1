import { describe, expect, it, vi } from "vitest";
import type { TimelineTrackJson } from "../api/client";
import { frameOffsetTime, frameTime } from "./frame_time";
import { previewCoverLayout } from "./preview_layout";
import { resumePreviewClock } from "./preview_clock";
import { timelineRuntimeSignature } from "./preview_timeline_signature";
import { videoFadeAnimation } from "./video_fade";
import { duckedPreviewVolume, subtitlePreviewPreset } from "./preview_presentation";

// @diffusionstudio/core touches Path2D at module load; install the stub before
// the engine (and its transitive core import) is evaluated.
const stubPath2D = vi.hoisted(() => {
  if (typeof globalThis.Path2D === "undefined") {
    vi.stubGlobal("Path2D", class Path2DStub {});
  }
  return undefined;
});
void stubPath2D;

const { DiffusionPreviewEngine } = await import("./diffusion_preview_engine");

describe("DiffusionPreviewEngine frame adapter", () => {
  it("始终以整数帧字符串传给 Diffusion Core，不引入第二套持久时间基", () => {
    expect(frameTime(0)).toBe("0f");
    expect(frameTime(12.6)).toBe("13f");
    expect(frameTime(-8)).toBe("0f");
  });

  it("素材中段镜头使用有符号内部偏移，保持时间线起点不被 source range 再次平移", () => {
    const timelineStartFrame = 100;
    const sourceStartFrame = 109;

    expect(frameOffsetTime(timelineStartFrame - sourceStartFrame)).toBe("-9f");
    expect(frameOffsetTime(276 - 102)).toBe("174f");
  });

  it("横屏和竖屏素材都采用 cover 轴，避免左右留下空白", () => {
    expect(previewCoverLayout(2560, 1080, 1920, 1080)).toEqual({ height: "100%" });
    expect(previewCoverLayout(1080, 1920, 1920, 1080)).toEqual({ width: "100%" });
  });

  it("播放前等待浏览器音频时钟恢复，避免按钮进入播放态但时间停在 0", async () => {
    let state = "suspended";
    const resume = vi.fn(async () => {
      state = "running";
    });

    const clock = {
      get state() {
        return state;
      },
      resume
    };

    await resumePreviewClock(clock);
    await resumePreviewClock(clock);

    expect(state).toBe("running");
    expect(resume).toHaveBeenCalledTimes(1);
  });

  it("浏览器不放行音频时钟时快速失败，不让播放按钮永久等待", async () => {
    const clock = {
      state: "suspended",
      resume: vi.fn(() => new Promise<void>(() => undefined))
    };

    await expect(resumePreviewClock(clock, 5)).rejects.toThrow(
      "浏览器未允许启动音频预览时钟"
    );
  });

  it("相同时间线引用变化时运行签名不变，真实参数变化时签名变化", () => {
    const timeline = {
      fps: 30,
      duration_frames: 60,
      tracks: [{
        track_id: "visual_base",
        track_type: "video",
        clips: [{
          timeline_clip_id: "clip_a",
          asset_id: "asset_a",
          timeline_start_frame: 0,
          timeline_end_frame: 60,
          source_start_frame: 10,
          source_end_frame: 70
        }]
      }]
    };
    const clone = structuredClone(timeline);
    const changed = structuredClone(timeline);
    changed.tracks[0].clips[0].source_start_frame = 11;

    expect(timelineRuntimeSignature(clone)).toBe(timelineRuntimeSignature(timeline));
    expect(timelineRuntimeSignature(changed)).not.toBe(timelineRuntimeSignature(timeline));
  });

  it("视频淡入淡出使用与整数帧时间线一致的 opacity 关键帧", async () => {
    expect(videoFadeAnimation(90, 15, 30)).toEqual({
      key: "opacity",
      frames: [
        { time: "0f", value: 0 },
        { time: "15f", value: 100 },
        { time: "60f", value: 100 },
        { time: "90f", value: 0 }
      ]
    });
    expect(videoFadeAnimation(90, 15, 0)?.frames.at(-1)).toEqual({ time: "90f", value: 100 });
    expect(videoFadeAnimation(90, 0, 30)?.frames).toEqual([
      { time: "0f", value: 100 },
      { time: "60f", value: 100 },
      { time: "90f", value: 0 }
    ]);
    expect(videoFadeAnimation(90, 0, 0)).toBeNull();

    vi.stubGlobal("Path2D", class Path2DStub {});
    const { ImageClip } = await import("@diffusionstudio/core");
    const clip = new ImageClip(undefined, {
      duration: "90f",
      animations: [videoFadeAnimation(90, 15, 30)!]
    });
    clip.animate(0.5);
    expect(clip.opacity).toBe(100);
    clip.animate(2.5);
    expect(clip.opacity).toBe(50);
  });

  it("五种字幕样式与服务端 preset 保持一致，并支持运行时切换", () => {
    expect(subtitlePreviewPreset("default")).toMatchObject({ y: "92%", fontSize: 42, outline: 3, bold: false });
    expect(subtitlePreviewPreset("large_center")).toMatchObject({ y: "50%", fontSize: 62, outline: 4, bold: true });
    expect(subtitlePreviewPreset("top_bar")).toMatchObject({
      y: "7%",
      fontSize: 44,
      outline: 1,
      bold: true,
      assBorderStyle: 3,
      background: { fill: "#000000", opacity: 44 }
    });
    expect(subtitlePreviewPreset("default").assBorderStyle).toBe(1);
    expect(subtitlePreviewPreset("minimal")).toMatchObject({ y: "94%", fontSize: 36, outline: 1, bold: false });
    expect(subtitlePreviewPreset("bold_bottom")).toMatchObject({ y: "91%", fontSize: 52, outline: 5, bold: true });
    expect(subtitlePreviewPreset(undefined)).toEqual(subtitlePreviewPreset("default"));
    expect(subtitlePreviewPreset("bold_bottom", 540).fontSize).toBe(26);
  });

  it("BGM ducking 响应两个触发轨、平滑边界、重叠区间与运行时切换", () => {
    const bgm = {
      track_id: "bgm",
      clips: [{ timeline_clip_id: "bgm", timeline_start_frame: 0, timeline_end_frame: 120 }],
      ducking: { enabled: true, duck_db: -12, trigger_tracks: ["voiceover", "original_audio"] }
    };
    const timeline = {
      fps: 30,
      duration_frames: 120,
      tracks: [
        bgm,
        { track_id: "voiceover", clips: [{ timeline_start_frame: 10, timeline_end_frame: 30 }] },
        { track_id: "original_audio", clips: [{ timeline_start_frame: 60, timeline_end_frame: 90 }] }
      ]
    };
    const ducked = 10 ** (-12 / 20);

    expect(duckedPreviewVolume(timeline, bgm, 1, 0)).toBe(1);
    expect(duckedPreviewVolume(timeline, bgm, 1, 20)).toBeCloseTo(ducked);
    expect(duckedPreviewVolume(timeline, bgm, 1, 70)).toBeCloseTo(ducked);
    expect(duckedPreviewVolume(timeline, bgm, 1, 10)).toBe(1);
    expect(duckedPreviewVolume(timeline, bgm, 1, 10.225)).toBeCloseTo((1 + ducked) / 2);
    expect(duckedPreviewVolume(timeline, bgm, 1, 33.75)).toBeCloseTo((1 + ducked) / 2);
    expect(duckedPreviewVolume(timeline, bgm, 1, 37.5)).toBe(1);
    timeline.tracks[2].clips = [{ timeline_start_frame: 25, timeline_end_frame: 40 }];
    expect(duckedPreviewVolume(timeline, bgm, 1, 33.75)).toBeCloseTo(ducked);
    bgm.ducking.enabled = false;
    expect(duckedPreviewVolume(timeline, bgm, 1, 20)).toBe(1);
    bgm.ducking.enabled = true;
    bgm.ducking.duck_db = -6;
    expect(duckedPreviewVolume(timeline, bgm, 1, 20)).toBeCloseTo(10 ** (-6 / 20));
  });

  it("BGM ducking 将取消联动的有声主画面视为原声，但尊重静音、solo 与无声视频", () => {
    const bgm = {
      track_id: "bgm",
      clips: [{ timeline_start_frame: 0, timeline_end_frame: 120 }],
      ducking: { enabled: true, duck_db: -12, trigger_tracks: ["original_audio"] }
    };
    const originalAudio: TimelineTrackJson = { track_id: "original_audio", clips: [] };
    const visualBase = {
      track_id: "visual_base",
      clips: [{
        timeline_start_frame: 10, timeline_end_frame: 40,
        asset_kind: "video", linked: true
      }]
    };
    const timeline = { fps: 30, duration_frames: 120, tracks: [bgm, originalAudio, visualBase] };

    expect(duckedPreviewVolume(timeline, bgm, 1, 20)).toBeCloseTo(10 ** (-12 / 20));
    originalAudio.muted = true;
    expect(duckedPreviewVolume(timeline, bgm, 1, 20)).toBe(1);
    originalAudio.muted = false;
    expect(duckedPreviewVolume(timeline, bgm, 1, 20, true)).toBe(1);
    originalAudio.solo = true;
    expect(duckedPreviewVolume(timeline, bgm, 1, 20, true)).toBeCloseTo(10 ** (-12 / 20));
    visualBase.clips[0].linked = false;
    expect(duckedPreviewVolume(timeline, bgm, 1, 20)).toBeCloseTo(10 ** (-12 / 20));
    expect(duckedPreviewVolume(timeline, bgm, 1, 20, false, () => false)).toBe(1);
  });
});

// Regression: a subtitle clip previously crashed the entire preview build with
// "Cannot set property name of #<el> which has only a getter". TextClip exposes
// a read-only name property in @diffusionstudio/core@4.0.3, so the engine must
// not assign to it. The test builds a minimal engine against a stubbed
// Composition/host and forces the rebuild path through sync().
describe("DiffusionPreviewEngine subtitle clips", () => {
  it("字幕轨道同步时不写入 TextClip 只读的 name 字段", async () => {
    const { TextClip } = await import("@diffusionstudio/core");

    // TextClip pulls a 2D context for text measurement; jsdom has none.
    // Install a permissive proxy stub before constructing any TextClip.
    if (!HTMLCanvasElement.prototype.getContext.toString().includes("STUB")) {
      const stubbed = function (this: HTMLCanvasElement) {
        const handler: ProxyHandler<object> = {
          get(_target, prop) {
            if (prop === "measureText") {
              return () => ({ width: 10, actualBoundingBoxAscent: 12, actualBoundingBoxDescent: 4 });
            }
            return () => undefined;
          },
          set() {
            return true;
          }
        };
        return new Proxy(
          {
            canvas: this,
            imageSmoothingEnabled: false,
            textAlign: "left",
            textBaseline: "alphabetic",
            font: "",
            fillStyle: "",
            strokeStyle: "",
            lineWidth: 1,
            lineCap: "butt",
            lineJoin: "miter",
            miterLimit: 10,
            shadowOffsetX: 0,
            shadowOffsetY: 0,
            shadowBlur: 0,
            shadowColor: "",
            filter: "none",
            globalAlpha: 1,
            globalCompositeOperation: "source-over"
          },
          handler
        );
      };
      stubbed.toString = () => "STUB";
      HTMLCanvasElement.prototype.getContext = stubbed as typeof HTMLCanvasElement.prototype.getContext;
    }

    // Sanity-check the real package shape before exercising the engine.
    const probe = new TextClip({ text: "x", duration: "10f" });
    const descriptor = Object.getOwnPropertyDescriptor(
      Object.getPrototypeOf(probe),
      "name"
    );
    if (descriptor && !descriptor.writable && !descriptor.set) {
      // Bypass the TS read-only check so the test still triggers the real
      // runtime throw reported by the user.
      expect(() => {
        (probe as { name: string }).name = "should throw";
      }).toThrow();
    }

    // Build a minimal engine instance without touching the real Composition
    // constructor (which still grabs OffscreenCanvas-derived things). The bug is
    // in createRuntimeClip, so drive that path directly.
    const engine = Object.create(DiffusionPreviewEngine.prototype);
    (engine as any).composition = { width: 1920, height: 1080 };
    (engine as any).fps = 30;
    (engine as any).sources = new Map();
    (engine as any).runtimeClips = new Map();
    (engine as any).layers = new Map();

    const track = {
      track_id: "subtitles",
      track_type: "subtitle",
      clips: [
        {
          timeline_clip_id: "clip_sub_1",
          asset_id: "",
          timeline_start_frame: 0,
          timeline_end_frame: 60,
          text: "字幕文本",
          subtitle_style: "default"
        }
      ]
    } as any;
    const clipJson = track.clips[0];

    const runtime = await (engine as any).createRuntimeClip(track, clipJson, null, false);
    expect(runtime).not.toBeNull();
    expect(runtime.clip).toBeInstanceOf(TextClip);
    expect(typeof runtime.clip.id).toBe("string");
  });
});
