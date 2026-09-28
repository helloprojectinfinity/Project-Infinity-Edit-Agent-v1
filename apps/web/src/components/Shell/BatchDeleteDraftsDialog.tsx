import * as Dialog from "@radix-ui/react-dialog";
import type { ReactElement } from "react";

type DraftDeleteEntry = { draft_id: string; name: string };

type BatchDeleteDraftsDialogProps = {
  open: boolean;
  drafts: DraftDeleteEntry[];
  pending: boolean;
  failed: boolean;
  onCancel: () => void;
  onConfirm: () => void;
};

export function BatchDeleteDraftsDialog({
  open,
  drafts,
  pending,
  failed,
  onCancel,
  onConfirm
}: BatchDeleteDraftsDialogProps): ReactElement {
  const visibleDrafts = drafts.slice(0, 3);
  const hiddenCount = Math.max(0, drafts.length - visibleDrafts.length);

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !pending) {
          onCancel();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="rx-overlay fixed inset-0 z-30 bg-black/60 backdrop-blur-sm" />
        <Dialog.Content
          className="rx-content fixed left-1/2 top-1/2 z-40 w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl bg-raised p-5 shadow-overlay focus:outline-none"
        >
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (!pending && drafts.length > 0) {
                onConfirm();
              }
            }}
          >
            <Dialog.Title className="text-lg font-semibold text-fg">
              刪除 {drafts.length} 條草稿？
            </Dialog.Title>
            <Dialog.Description className="mt-2 text-sm leading-6 text-fg-muted">
              刪除後這些草稿將不再出現在列表中，目前無法在介面中撤銷。
            </Dialog.Description>

            <div className="mt-4 rounded-lg border border-danger/40 bg-danger/10 px-3 py-2.5">
              <ul className="space-y-1 text-sm text-fg" aria-label="將刪除的草稿">
                {visibleDrafts.map((draft) => (
                  <li className="truncate" key={draft.draft_id}>
                    {draft.name}
                  </li>
                ))}
              </ul>
              {hiddenCount > 0 ? (
                <p className="mt-1 text-xs text-fg-muted">另有 {hiddenCount} 條草稿</p>
              ) : null}
            </div>

            {failed ? (
              <p className="mt-4 rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                未刪除任何草稿，已保留當前選擇，請重試。
              </p>
            ) : null}

            <div className="mt-5 flex justify-end gap-2">
              <button
                className="rounded-md border border-line px-3 py-2 text-sm text-fg-muted transition-colors ease-standard hover:bg-hover hover:text-fg disabled:opacity-40"
                type="button"
                onClick={onCancel}
                disabled={pending}
              >
                取消
              </button>
              <button
                className="rounded-md bg-danger px-3 py-2 text-sm font-medium text-white transition-colors ease-standard hover:bg-danger/80 disabled:opacity-40"
                type="submit"
                disabled={pending || drafts.length === 0}
              >
                {pending ? "正在刪除" : `刪除 ${drafts.length} 條草稿`}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
