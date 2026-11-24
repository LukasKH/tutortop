import { ChatMark, ChatRole, ChatStatus, type ChatItem, type Working } from "@/lib/api";

/**
 * Inserts or replaces a chat item by id (streaming text and tool updates reuse
 * the id). A tool call that fails leaves the chat.
 */
export function upsertChat(items: ChatItem[], item: ChatItem): ChatItem[] {
  if (failedTool(item)) return items.filter((x) => x.id !== item.id);
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
  const i = items.findIndex((x) => x.mark === ChatMark.MarkPlanApproved);
  return i === -1 ? items : items.slice(i + 1);
}

/**
 * Merges the saved transcript with what we have live. Items still streaming
 * (assistant text, running tools) aren't saved yet, so we keep them at the end.
 * Transcripts saved by older versions can hold failed tool calls; they're dropped.
 */
export function mergeChat(saved: ChatItem[] | null, live: ChatItem[]): ChatItem[] {
  const out = (saved ?? []).filter((x) => !failedTool(x));
  const ids = new Set(out.map((x) => x.id));
  return [...out, ...live.filter((x) => !ids.has(x.id))];
}

/**
 * Whether a conversation shows that its tutor is thinking: the tutor is
 * working and nothing in the conversation moves already. A running tool call
 * has its own spinner, and a reply moves as it streams in. Tool calls at the
 * end run together, so any of them still running counts. What the tutor is
 * working on always shows: the backend clears it once a message or tool call
 * takes over.
 */
export function showThinking(items: ChatItem[], busy: boolean, working?: Working): boolean {
  if (!busy) return false;
  if (working?.thinking || working?.preparing) return true;
  if (items.at(-1)?.role === ChatRole.RoleAssistant) return false;
  for (let i = items.length - 1; i >= 0 && items[i].role === ChatRole.RoleTool; i--) {
    if (items[i].status === ChatStatus.ChatRunning) return false;
  }
  return true;
}

/**
 * One line for what the tutor is working on: the tool call it's writing, or
 * the latest line of its reasoning without Markdown's marks. Null for neither.
 */
export function workingLine(working: Working | undefined): string | null {
  if (working?.preparing) return `${working.preparing}…`;
  return (working?.thinking ?? "").split("\n").map(plainLine).findLast(Boolean) ?? null;
}

/** A line of Markdown as plain text: "- **Next** `step`" reads "Next step". */
const plainLine = (line: string) =>
  line
    .replace(/\*\*|__|`/g, "")
    .replace(/^[\s#>*-]+/, "")
    .trim();

/**
 * A tool call that failed, such as a shell command the policy refused. The
 * tutor recovers on its own, so the learner doesn't see it; real problems
 * come as notices.
 */
function failedTool(item: ChatItem): boolean {
  return item.role === ChatRole.RoleTool && item.status === ChatStatus.ChatFailed;
}
