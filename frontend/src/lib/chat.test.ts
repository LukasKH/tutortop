import { expect, test } from "vitest";
import { ChatRole, type ChatItem } from "@/lib/api";
import { afterSetup, mergeChat, showThinking, upsertChat } from "@/lib/chat";

const item = (id: string, over: Partial<ChatItem> = {}): ChatItem => ({ id, thread: "plan", role: ChatRole.RoleAssistant, text: id, at: "", ...over });

test("a streamed update replaces its message in place", () => {
  const items = [item("1"), item("2", { text: "Hel" })];
  expect(upsertChat(items, item("2", { text: "Hello" })).map((x) => x.text)).toEqual(["1", "Hello"]);
  expect(upsertChat(items, item("3")).map((x) => x.id)).toEqual(["1", "2", "3"]);
});

test("loading a transcript keeps live messages that aren't saved yet", () => {
  const saved = [item("1"), item("2")];
  const live = [item("2"), item("3", { text: "streaming" })];
  expect(mergeChat(saved, live).map((x) => x.id)).toEqual(["1", "2", "3"]);
  expect(mergeChat(null, live)).toEqual(live);
});

test("thinking shows while the tutor works, unless a reply streams or a tool call runs", () => {
  const asked = item("q", { role: ChatRole.RoleUser });
  const tool = (id: string, status: string) => item(id, { role: ChatRole.RoleTool, status });
  expect(showThinking([asked], true)).toBe(true);
  expect(showThinking([], true)).toBe(true);
  expect(showThinking([asked], false)).toBe(false);
  expect(showThinking([asked, item("reply")], true)).toBe(false);
  // Calls at the end run together: an earlier one still running is still moving.
  expect(showThinking([asked, tool("a", "running"), tool("b", "done")], true)).toBe(false);
  expect(showThinking([asked, tool("a", "done"), tool("b", "failed")], true)).toBe(true);
  expect(showThinking([tool("a", "running"), item("reply"), item("n", { role: ChatRole.RoleNotice })], true)).toBe(true);
});

test("the setup interview is hidden up to the plan's approval", () => {
  const items = [item("q", { role: ChatRole.RoleUser }), item("n", { role: ChatRole.RoleNotice, text: "You approved the plan." }), item("after")];
  expect(afterSetup(items).map((x) => x.id)).toEqual(["after"]);
  expect(afterSetup([item("1")]).map((x) => x.id)).toEqual(["1"]);
});
