import { expect, test } from "vitest";
import { ChatRole, type ChatItem } from "@/lib/api";
import { afterSetup, mergeChat, upsertChat } from "@/lib/chat";

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

test("the setup interview is hidden up to the plan's approval", () => {
  const items = [item("q", { role: ChatRole.RoleUser }), item("n", { role: ChatRole.RoleNotice, text: "You approved the plan." }), item("after")];
  expect(afterSetup(items).map((x) => x.id)).toEqual(["after"]);
  expect(afterSetup([item("1")]).map((x) => x.id)).toEqual(["1"]);
});
