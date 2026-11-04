import { ChatMark, ChatRole, type ChatItem } from "@/lib/api";

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
 * the learner approving it, stay with the tutor but off screen. The approval
 * is the notice WorkspaceService.ApproveProposal posts, marked MarkPlanApproved.
 */
export function afterSetup(items: ChatItem[]): ChatItem[] {
  const i = items.findIndex(isPlanApproval);
  return i === -1 ? items : items.slice(i + 1);
}

// Notices saved before items had marks are known by their text.
const isPlanApproval = (x: ChatItem) =>
  x.mark === ChatMark.MarkPlanApproved || (!x.mark && x.role === ChatRole.RoleNotice && x.text.startsWith("You approved the plan"));

/**
 * Merges the saved transcript with what we have live. Items still streaming
 * (assistant text, running tools) aren't saved yet, so we keep them at the end.
 */
export function mergeChat(saved: ChatItem[] | null, live: ChatItem[]): ChatItem[] {
  const out = saved ?? [];
  const ids = new Set(out.map((x) => x.id));
  return [...out, ...live.filter((x) => !ids.has(x.id))];
}

/**
 * Whether a conversation shows that its tutor is thinking: the tutor is
 * working and nothing in the conversation moves already. A running tool call
 * has its own spinner, and a reply moves as it streams in. Tool calls at the
 * end run together, so any of them still running counts.
 */
export function showThinking(items: ChatItem[], busy: boolean): boolean {
  if (!busy || items.at(-1)?.role === "assistant") return false;
  for (let i = items.length - 1; i >= 0 && items[i].role === "tool"; i--) {
    if (items[i].status === "running") return false;
  }
  return true;
}
