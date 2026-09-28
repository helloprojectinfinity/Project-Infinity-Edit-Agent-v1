import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode, useEffect, useState } from "react";
import type { ReactElement } from "react";
import { createRoot } from "react-dom/client";
import { queryClient } from "./app/query_client";
import { router } from "./app/router";
import {
  AUTH_CHANGED_EVENT,
  AUTH_REQUIRED_EVENT,
  bootstrapAuthFromLaunchUrl,
  getAuthToken,
  listenForLaunchUrlAuth
} from "./auth";
import { initWebVitals } from "./perf/vitals";
import "./index.css";

bootstrapAuthFromLaunchUrl();

export function AppRoot(): ReactElement {
  const [hasToken, setHasToken] = useState(() => Boolean(getAuthToken()));

  useEffect(() => {
    const syncTokenState = () => setHasToken(Boolean(getAuthToken()));
    const stopListeningForLaunchUrlAuth = listenForLaunchUrlAuth();
    window.addEventListener(AUTH_CHANGED_EVENT, syncTokenState);
    window.addEventListener(AUTH_REQUIRED_EVENT, syncTokenState);
    return () => {
      stopListeningForLaunchUrlAuth();
      window.removeEventListener(AUTH_CHANGED_EVENT, syncTokenState);
      window.removeEventListener(AUTH_REQUIRED_EVENT, syncTokenState);
    };
  }, []);

  if (!hasToken) {
    return <LaunchGuide />;
  }

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

function LaunchGuide(): ReactElement {
  return (
    <main className="grid min-h-screen place-items-center bg-ink px-6">
      <section className="w-full max-w-lg rounded-lg border border-line bg-panel p-8">
        <p className="text-sm font-medium text-fg-muted">需要啟動授權</p>
        <h1 className="mt-3 text-2xl font-semibold text-fg">請從後端啟動網址開啟 Rushes</h1>
        <p className="mt-4 leading-7 text-fg-muted">
          目前頁面未收到啟動 token。請使用後端程式顯示的本地網址開啟應用程式，網址包含
          <code className="mx-1 rounded bg-raised px-1.5 py-0.5 text-sm text-fg">#t=&lt;token&gt;</code>
          ，瀏覽器會儲存 token。首次授權後，可直接開啟不含 token 的本地網址。
        </p>
      </section>
    </main>
  );
}

const rootElement = document.getElementById("root");
if (rootElement) {
  createRoot(rootElement).render(
    <StrictMode>
      <AppRoot />
    </StrictMode>
  );
}

// 开发期采集 Web Vitals 到 console（生产构建 tree-shake，零开销）。
initWebVitals();
