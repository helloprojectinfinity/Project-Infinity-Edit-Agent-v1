import { Sparkles } from "lucide-react";
import type { ReactElement, ReactNode } from "react";
import type { AffectedMemory } from "../../api/client";

// MemoryCardShell 是长期记忆相关卡片的共用外壳:强调色描边 + Sparkles 图标 + 正文区。
// M3 的「已记住/已移除」通知与 M4 的「撤回被回退对话形成的记忆」共用同一视觉语言,让
// 用户一眼认出这是记忆卡片。仅是纯展示外壳,不含任何行为。
export function MemoryCardShell({
  children,
  testId
}: {
  children: ReactNode;
  testId?: string;
}): ReactElement {
  return (
    <article
      data-testid={testId}
      className="flex w-full items-start gap-2 rounded-md border border-accent/25 bg-accent/5 px-3 py-2 text-[13px] leading-5 text-fg"
    >
      <Sparkles size={14} strokeWidth={1.8} aria-hidden className="mt-0.5 shrink-0 text-accent" />
      <div className="min-w-0 flex-1">{children}</div>
    </article>
  );
}

// AffectedMemoriesCard 在「编辑并重发」回退后出现:列出证据落在被撤回对话里、且创建于
// 同一区间内的长期记忆(后端在回退事务内算出)。默认保留——不操作即留存;「撤回这些记忆」
// 走 Actor=User 删除路径,撤回后不可恢复、无二次确认;「保留」只关闭卡片、不动记忆。
export function AffectedMemoriesCard({
  memories,
  onRetract,
  onDismiss,
  retracting
}: {
  memories: AffectedMemory[];
  onRetract: () => void;
  onDismiss: () => void;
  retracting: boolean;
}): ReactElement {
  return (
    <MemoryCardShell testId="affected-memories-card">
      <p className="font-medium">這些長期記憶來自被撤回的對話</p>
      <p className="mt-0.5 text-xs text-fg-muted">
        剛才的編輯並重發撤回了下面這些記憶所依據的對話。預設保留；若不該記住，可一併撤回。
      </p>
      <ul className="mt-1.5 space-y-1">
        {memories.map((memory) => (
          <li key={memory.key} className="break-words text-xs text-fg-muted">
            <span className="font-mono text-fg">{memory.key}</span>
            <span className="text-fg-faint">：</span>
            {memory.statement}
          </li>
        ))}
      </ul>
      <p className="mt-1.5 text-2xs text-danger">撤回後不可復原。</p>
      <div className="mt-1.5 flex items-center gap-2">
        <button
          type="button"
          className="rounded-sm border border-danger/40 bg-danger/10 px-2 py-1 text-xs font-medium text-danger transition-colors hover:bg-danger/15 disabled:opacity-45"
          onClick={onRetract}
          disabled={retracting}
        >
          {retracting ? "撤回中…" : "撤回這些記憶"}
        </button>
        <button
          type="button"
          className="rounded-sm px-2 py-1 text-xs text-fg-muted transition-colors hover:bg-hover hover:text-fg disabled:opacity-45"
          onClick={onDismiss}
          disabled={retracting}
        >
          保留
        </button>
      </div>
    </MemoryCardShell>
  );
}
