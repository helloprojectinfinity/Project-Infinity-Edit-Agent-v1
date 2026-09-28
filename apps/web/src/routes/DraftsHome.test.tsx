import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  useParams
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { storeAuthToken } from "../auth";
import { DraftsHomePage } from "./DraftsHome";

class NoopEventSource {
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  addEventListener(): void {}
  removeEventListener(): void {}
  close(): void {}
}

describe("DraftsHomePage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    window.sessionStorage.clear();
  });

  it("渲染草稿卡片墙：草稿名、素材计数与开始创作入口", async () => {
    renderHome();

    expect(await screen.findByText("7月7日")).toBeTruthy();
    expect(await screen.findByText("旅行 Vlog")).toBeTruthy();
    expect(screen.getByText("開始創作")).toBeTruthy();
    await waitFor(() => expect(screen.getByText(/3 個素材/)).toBeTruthy());
  });

  it("空草稿列表显示空态引导", async () => {
    renderHome({ drafts: [] });

    expect(await screen.findByText("還沒有草稿")).toBeTruthy();
  });

  it("点击更多操作展开卡片下拉菜单", async () => {
    renderHome();

    await screen.findByText("7月7日");
    // radix DropdownMenu 由键盘/指針打開（並非裸 click），用 Enter 鍵觸發 Trigger。
    const trigger = await screen.findByLabelText("草稿 7月7日 更多操作");
    fireEvent.keyDown(trigger, { key: "Enter" });

    await waitFor(() => expect(screen.getByText("重新命名")).toBeTruthy());
    expect(screen.getByText("複製")).toBeTruthy();
    expect(screen.getByText("刪除")).toBeTruthy();
  });

  it("右键卡片打开上下文菜单", async () => {
    renderHome();

    const title = await screen.findByText("7月7日");
    fireEvent.contextMenu(title);

    await waitFor(() => expect(screen.getByText("重新命名")).toBeTruthy());
    expect(screen.getByText("複製")).toBeTruthy();
    expect(screen.getByText("刪除")).toBeTruthy();
  });

  it("点击开始创作：新建草稿并跳转编辑器", async () => {
    renderHome();

    await screen.findByText("7月7日");
    screen.getByText("開始創作").click();

    expect(await screen.findByText("編輯器:draft_new")).toBeTruthy();
  });

  it("批次管理可选择草稿、二次确认并一次删除", async () => {
    renderHome();

    await screen.findByText("7月7日");
    fireEvent.click(screen.getByRole("button", { name: "批次管理" }));

    const first = await screen.findByRole("button", { name: "選擇草稿 7月7日" });
    const second = screen.getByRole("button", { name: "選擇草稿 旅行 Vlog" });
    expect(screen.getByRole("button", { name: "刪除所選" }).hasAttribute("disabled")).toBe(true);

    fireEvent.click(first);
    fireEvent.click(second);
    expect(first.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("2", { selector: "strong" })).toBeTruthy();
    expect(screen.queryByText("編輯器:draft_1")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "刪除所選" }));
    expect(await screen.findByRole("dialog", { name: "刪除 2 條草稿？" })).toBeTruthy();
    expect(screen.getByText("7月7日", { selector: "li" })).toBeTruthy();
    expect(screen.getByText("旅行 Vlog", { selector: "li" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "刪除 2 條草稿" }));
    expect(await screen.findByText("還沒有草稿")).toBeTruthy();
    expect(screen.queryByRole("dialog", { name: "刪除 2 條草稿？" })).toBeNull();
  });

  it("批次管理支持全选、取消全选与退出", async () => {
    renderHome();

    await screen.findByText("7月7日");
    fireEvent.click(screen.getByRole("button", { name: "批次管理" }));
    fireEvent.click(await screen.findByRole("button", { name: "全選" }));
    expect(screen.getByText("2", { selector: "strong" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "取消全選" }));
    expect(screen.getByText("0", { selector: "strong" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "退出" }));
    expect(screen.queryByRole("toolbar", { name: "草稿批次管理" })).toBeNull();
    expect(screen.getByRole("button", { name: "批次管理" })).toBeTruthy();
  });

  it("批次删除失败时保留选择和草稿并显示原子失败提示", async () => {
    renderHome({ batchDeleteFails: true });

    await screen.findByText("7月7日");
    fireEvent.click(screen.getByRole("button", { name: "批次管理" }));
    fireEvent.click(await screen.findByRole("button", { name: "選擇草稿 7月7日" }));
    fireEvent.click(screen.getByRole("button", { name: "刪除所選" }));
    fireEvent.click(await screen.findByRole("button", { name: "刪除 1 條草稿" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "未刪除任何草稿，已保留當前選擇，請重試。"
    );
    expect(screen.getByText("7月7日", { selector: "li" })).toBeTruthy();
    expect(screen.getByText("7月7日", { selector: "span" })).toBeTruthy();
  });

  it("点击设置打开全局设置弹窗，关闭后消失", async () => {
    renderHome();

    await screen.findByText("7月7日");
    expect(screen.queryByRole("dialog", { name: "全域設定" })).toBeNull();

    screen.getByRole("button", { name: "設定" }).click();
    expect(await screen.findByRole("dialog", { name: "全域設定" })).toBeTruthy();

    screen.getByRole("button", { name: "關閉設定" }).click();
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "全域設定" })).toBeNull());
  });

  it("長期記憶面板支援逐條刪除與確認後清空", async () => {
    renderHome({
      memories: [
        { memory_key: "pacing", kind: "preference", statement: "成片節奏偏快" },
        { memory_key: "subtitle_style", kind: "correction", statement: "字幕不要遮臉" }
      ]
    });

    await screen.findByText("7月7日");
    fireEvent.click(screen.getByRole("button", { name: "設定" }));
    expect(await screen.findByText("成片節奏偏快")).toBeTruthy();
    expect(screen.getByText("字幕不要遮臉")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "刪除長期記憶 pacing" }));
    await waitFor(() => expect(screen.queryByText("成片節奏偏快")).toBeNull());
    expect(screen.getByText("字幕不要遮臉")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "清空全部長期記憶" }));
    expect(await screen.findByRole("dialog", { name: "確認清空全部長期記憶？" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByText("字幕不要遮臉")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "清空全部長期記憶" }));
    fireEvent.click(await screen.findByRole("button", { name: "確認清空全部長期記憶" }));
    expect(await screen.findByText("還沒有長期記憶")).toBeTruthy();
  });

  it("逐條刪除失敗後清空成功會移除陳舊錯誤", async () => {
    renderHome({
      memories: [
        { memory_key: "pacing", kind: "preference", statement: "成片節奏偏快" },
        { memory_key: "subtitle_style", kind: "correction", statement: "字幕不要遮臉" }
      ],
      memoryDeleteFails: true
    });

    await screen.findByText("7月7日");
    fireEvent.click(screen.getByRole("button", { name: "設定" }));
    fireEvent.click(await screen.findByRole("button", { name: "刪除長期記憶 pacing" }));
    expect((await screen.findByRole("alert")).textContent).toContain("刪除失敗");

    fireEvent.click(screen.getByRole("button", { name: "清空全部長期記憶" }));
    fireEvent.click(await screen.findByRole("button", { name: "確認清空全部長期記憶" }));
    expect(await screen.findByText("還沒有長期記憶")).toBeTruthy();
    expect(screen.queryByText(/刪除失敗/)).toBeNull();
  });
});

type DraftFixture = {
  draft_id: string;
  name: string;
  material_count: number;
  cover_asset_ids?: string[];
};

type HomeFixture = {
  drafts?: DraftFixture[];
  batchDeleteFails?: boolean;
  memoryDeleteFails?: boolean;
  memories?: Array<{
    memory_key: string;
    kind: "preference" | "correction" | "habit";
    statement: string;
  }>;
};

function DraftMarker(): ReactElement {
  const params = useParams({ strict: false }) as { draftId?: string };
  return <div>編輯器:{params.draftId}</div>;
}

function renderHome(fixture: HomeFixture = {}): void {
  storeAuthToken("test-token");
  vi.stubGlobal("EventSource", NoopEventSource);
  vi.stubGlobal("fetch", mockFetch(fixture));

  const rootRoute = createRootRoute();
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: DraftsHomePage
  });
  const draftRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/drafts/$draftId",
    component: DraftMarker
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, draftRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] })
  });

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } }
  });
  render(
    <QueryClientProvider client={queryClient}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>
  );
}

function mockFetch(
  fixture: HomeFixture
): (input: RequestInfo | URL, init?: RequestInit) => Promise<Response> {
  let drafts = fixture.drafts ?? [
    { draft_id: "draft_1", name: "7月7日", material_count: 3, cover_asset_ids: ["asset_1", "asset_2"] },
    { draft_id: "draft_2", name: "旅行 Vlog", material_count: 0, cover_asset_ids: [] }
  ];
  let memories = (fixture.memories ?? []).map((memory) => ({
    ...memory,
    source_draft_id: "draft_1",
    created_at: "2026-07-07T00:00:00Z",
    last_confirmed_at: "2026-07-07T00:00:00Z"
  }));
  return (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    if (url === "/api/memories" && method === "GET") {
      return jsonResponse({ memories });
    }
    if (url.startsWith("/api/memories/") && method === "DELETE") {
      if (fixture.memoryDeleteFails) {
        return Promise.resolve(
          new Response(JSON.stringify({ detail: { reason: "internal_error" } }), {
            status: 500,
            headers: { "Content-Type": "application/json" }
          })
        );
      }
      const memoryKey = decodeURIComponent(url.slice("/api/memories/".length));
      const before = memories.length;
      memories = memories.filter((memory) => memory.memory_key !== memoryKey);
      return jsonResponse({
        deleted_count: before - memories.length,
        deleted_memory_keys: before === memories.length ? [] : [memoryKey]
      });
    }
    if (url === "/api/memories" && method === "DELETE") {
      const deleted = memories.map((memory) => memory.memory_key);
      memories = [];
      return jsonResponse({ deleted_count: deleted.length, deleted_memory_keys: deleted });
    }
    if (url === "/api/drafts" && method === "DELETE") {
      if (fixture.batchDeleteFails) {
        return Promise.resolve(
          new Response(JSON.stringify({ detail: { reason: "internal_error" } }), {
            status: 500,
            headers: { "Content-Type": "application/json" }
          })
        );
      }
      const payload = JSON.parse(String(init?.body)) as { draft_ids: string[] };
      const deleted = new Set(payload.draft_ids);
      drafts = drafts.filter((draft) => !deleted.has(draft.draft_id));
      return jsonResponse({
        deleted_count: payload.draft_ids.length,
        deleted_draft_ids: payload.draft_ids,
        event_ids: payload.draft_ids.map((_, index) => index + 1)
      });
    }
    if (url.includes("/api/drafts") && method === "POST") {
      return jsonResponse({ draft: { draft_id: "draft_new" }, event_ids: [] });
    }
    if (url.includes("/api/drafts")) {
      return jsonResponse({
        drafts: drafts.map((draft) => ({
          draft_id: draft.draft_id,
          name: draft.name,
          status: "active",
          updated_at: "2026-07-07T00:00:00Z",
          material_count: draft.material_count,
          cover_asset_ids: draft.cover_asset_ids ?? []
        }))
      });
    }
    return jsonResponse({});
  };
}

function jsonResponse(payload: unknown): Promise<Response> {
  return Promise.resolve(
    new Response(JSON.stringify(payload), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    })
  );
}
