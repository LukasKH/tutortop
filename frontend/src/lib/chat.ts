import type { ChatItem } from "@/lib/api";

/** Inserts or replaces a chat item by id (streaming text and tool updates reuse the id). */
export function upsertChat(items: ChatItem[], item: ChatItem): ChatItem[] {
  const i = items.findIndex((x) => x.id === item.id);
  if (i === -1) return [...items, item];
  const next = items.slice();
  next[i] = item;
  return next;
}

/**
 * The plan conversation after setup. The interview and the first plan, up to
 * the learner approving it, stay with the tutor but off screen. The match is
 * on the notice ApproveProposal posts (internal/app/app.go).
 */
export function afterSetup(items: ChatItem[]): ChatItem[] {
  const i = items.findIndex((x) => x.role === "notice" && x.text.startsWith("You approved the plan"));
  return i === -1 ? items : items.slice(i + 1);
}

/**
 * Merges the saved transcript with what we have live. Items still streaming
 * (assistant text, running tools) aren't saved yet, so we keep them at the end.
 */
export function mergeChat(saved: ChatItem[] | null, live: ChatItem[]): ChatItem[] {
  const out = saved ?? [];
  const ids = new Set(out.map((x) => x.id));
  return [...out, ...live.filter((x) => !ids.has(x.id))];
}
