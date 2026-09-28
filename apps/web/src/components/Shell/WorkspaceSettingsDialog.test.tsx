import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { storeAuthToken } from "../../auth";
import { WorkspaceSettingsDialog } from "./WorkspaceSettingsDialog";

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

function memoryPayload(statement: string, manuallyRevisedAt: string) {
  return {
    memory_key: "pacing",
    kind: "preference",
    statement,
    source_draft_id: "draft_1",
    created_at: "2026-07-18T00:00:00Z",
    last_confirmed_at: "2026-07-18T00:00:00Z",
    manually_revised_at: manuallyRevisedAt
  };
}

describe("WorkspaceSettingsDialog 長期記憶就地編輯", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("就地編輯儲存期間「正在儲存」可見，settled 後顯示新 statement 與「手動修訂」", async () => {
    storeAuthToken("e2e-token");
    let statement = "成片節奏偏快";
    let revisedAt = "";
    let releasePatch: () => void = () => {};
    const patchGate = new Promise<void>((resolve) => {
      releasePatch = resolve;
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (url.endsWith("/api/memories") && method === "GET") {
        return jsonResponse({ memories: [memoryPayload(statement, revisedAt)] });
      }
      if (url.includes("/api/memories/pacing") && method === "PATCH") {
        statement = JSON.parse(String(init?.body)).statement as string;
        revisedAt = "2026-07-18T01:00:00Z";
        await patchGate; // 暫緩 settled，讓「正在儲存」可被觀察
        return jsonResponse(memoryPayload(statement, revisedAt));
      }
      return jsonResponse({});
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <WorkspaceSettingsDialog open onClose={vi.fn()} />
      </QueryClientProvider>
    );

    await screen.findByText("成片節奏偏快");
    fireEvent.click(screen.getByRole("button", { name: "編輯長期記憶 pacing" }));
    fireEvent.change(screen.getByRole("textbox", { name: "編輯長期記憶 pacing" }), {
      target: { value: "使用者手動改為整體更緊湊" }
    });
    fireEvent.click(screen.getByRole("button", { name: "儲存" }));

    // 儲存 pending 期間保持編輯態且「正在儲存」可見。
    expect(await screen.findByRole("button", { name: "正在儲存" })).toBeTruthy();

    releasePatch();

    // settled 後退出編輯態，列表顯示新 statement 與「手動修訂」標。
    await screen.findByText("使用者手動改為整體更緊湊");
    expect(await screen.findByText("手動修訂")).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("button", { name: "正在儲存" })).toBeNull());
  });

  it("儲存失敗保留編輯態與草稿文字，並顯示錯誤提示", async () => {
    storeAuthToken("e2e-token");
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (url.endsWith("/api/memories") && method === "GET") {
        return jsonResponse({ memories: [memoryPayload("成片節奏偏快", "")] });
      }
      if (url.includes("/api/memories/pacing") && method === "PATCH") {
        return new Response(JSON.stringify({ detail: "boom" }), { status: 500 });
      }
      return jsonResponse({});
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <WorkspaceSettingsDialog open onClose={vi.fn()} />
      </QueryClientProvider>
    );

    await screen.findByText("成片節奏偏快");
    fireEvent.click(screen.getByRole("button", { name: "編輯長期記憶 pacing" }));
    const textarea = screen.getByRole("textbox", { name: "編輯長期記憶 pacing" });
    fireEvent.change(textarea, { target: { value: "改了一半的草稿" } });
    fireEvent.click(screen.getByRole("button", { name: "儲存" }));

    await screen.findByText(/儲存失敗/);
    // 仍在編輯態，草稿文字保留。
    expect(
      (screen.getByRole("textbox", { name: "編輯長期記憶 pacing" }) as HTMLTextAreaElement).value
    ).toBe("改了一半的草稿");
  });
});
