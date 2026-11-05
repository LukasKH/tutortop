import { expect, test } from "vitest";
import { ChatMark, ChatRole, ChatStatus, type ChatItem } from "@/lib/api";
import { afterSetup, mergeChat, showThinking } from "@/lib/chat";

const item = (id: string, over: Partial<ChatItem> = {}): ChatItem => ({ id, thread: "plan", role: ChatRole.RoleAssistant, text: id, at: "", ...over });

test("loading a transcript keeps live messages that aren't saved yet", () => {
  const saved = [item("1"), item("2")];
  const live = [item("2"), item("3", { text: "streaming" })];
  expect(mergeChat(saved, live).map((x) => x.id)).toEqual(["1", "2", "3"]);
  expect(mergeChat(null, live)).toEqual(live);
});

test("thinking shows while the tutor works, unless a reply streams or a tool call runs", () => {
  const asked = item("q", { role: ChatRole.RoleUser });
  const tool = (id: string, status: ChatStatus) => item(id, { role: ChatRole.RoleTool, status });
  expect(showThinking([asked], true)).toBe(true);
  expect(showThinking([], true)).toBe(true);
  expect(showThinking([asked, item("reply")], true)).toBe(false);
  // Calls at the end run together: an earlier one still running is still moving.
  expect(showThinking([asked, tool("a", ChatStatus.ChatRunning), tool("b", ChatStatus.ChatDone)], true)).toBe(false);
  expect(showThinking([asked, tool("a", ChatStatus.ChatDone), tool("b", ChatStatus.ChatFailed)], true)).toBe(true);
  expect(showThinking([tool("a", ChatStatus.ChatRunning), item("reply"), item("n", { role: ChatRole.RoleNotice })], true)).toBe(true);
});

test("the setup interview is hidden up to the plan's approval, found by its mark or, in older chats, its text", () => {
  const interview = item("q", { role: ChatRole.RoleUser, text: "You approved the plan, right?" });
  const approved = item("n", { role: ChatRole.RoleNotice, text: "Plan approved.", mark: ChatMark.MarkPlanApproved });
  const old = item("n", { role: ChatRole.RoleNotice, text: "You approved the plan (revision 1)." });
  expect(afterSetup([interview, approved, item("after")]).map((x) => x.id)).toEqual(["after"]);
  expect(afterSetup([interview, old, item("after")]).map((x) => x.id)).toEqual(["after"]);
  expect(afterSetup([item("1")]).map((x) => x.id)).toEqual(["1"]);
});
