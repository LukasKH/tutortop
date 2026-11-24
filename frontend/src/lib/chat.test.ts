import { expect, test } from "vitest";
import { ChatMark, ChatRole, ChatStatus, type ChatItem, type Working } from "@/lib/api";
import { afterSetup, mergeChat, showThinking, upsertChat, workingLine } from "@/lib/chat";

const item = (id: string, over: Partial<ChatItem> = {}): ChatItem => ({ id, thread: "plan", role: ChatRole.RoleAssistant, text: id, at: "", ...over });

test("loading a transcript keeps live messages that aren't saved yet", () => {
  const saved = [item("1"), item("2")];
  const live = [item("2"), item("3", { text: "streaming" })];
  expect(mergeChat(saved, live).map((x) => x.id)).toEqual(["1", "2", "3"]);
  expect(mergeChat(null, live)).toEqual(live);
});

test("a failed tool call leaves the chat, live or saved; a failure notice stays", () => {
  const blocked = item("t", { role: ChatRole.RoleTool, text: "curl.exe -sI https://go.dev", status: ChatStatus.ChatFailed });
  const notice = item("n", { role: ChatRole.RoleNotice, text: "Agent crashed", status: ChatStatus.ChatFailed });
  const running = upsertChat([item("1")], { ...blocked, status: ChatStatus.ChatRunning });
  expect(running.map((x) => x.id)).toEqual(["1", "t"]);
  expect(upsertChat(running, blocked).map((x) => x.id)).toEqual(["1"]);
  expect(upsertChat([item("1")], blocked).map((x) => x.id)).toEqual(["1"]);
  expect(upsertChat([item("1")], notice).map((x) => x.id)).toEqual(["1", "n"]);
  expect(mergeChat([item("1"), blocked, notice], []).map((x) => x.id)).toEqual(["1", "n"]);
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
  // What the tutor works on after a reply shows; the backend clears it once the next message or tool call arrives.
  expect(showThinking([asked, item("reply")], true, working({ preparing: "Drafting your plan" }))).toBe(true);
  expect(showThinking([asked, item("reply")], true, working({ thinking: "  \n\n" }))).toBe(false);
  expect(showThinking([asked, item("reply")], true, working({}))).toBe(false);
  expect(showThinking([asked], false, working({ thinking: "hmm" }))).toBe(false);
});

const working = (over: Partial<Working>): Working => ({ workspace: "ws", thread: "plan", thinking: "", preparing: "", ...over });

test("the working line is the call being written, else the latest line of reasoning without Markdown", () => {
  expect(workingLine(working({ thinking: "**Reading notes**", preparing: "Drafting your plan" }))).toBe("Drafting your plan…");
  expect(workingLine(working({ thinking: "**Reading notes**\n\nThe learner knows `go test`.\n\n" }))).toBe("The learner knows go test.");
  expect(workingLine(working({ thinking: "## Planning\n- **next** step" }))).toBe("next step");
  expect(workingLine(working({ thinking: "Check syntax\n```go\n" }))).toBe("Check syntax");
  expect(workingLine(working({}))).toBeNull();
  expect(workingLine(undefined)).toBeNull();
});

test("the setup interview is hidden up to the plan's approval, found by its mark", () => {
  const interview = item("q", { role: ChatRole.RoleUser, text: "You approved the plan, right?" });
  const approved = item("n", { role: ChatRole.RoleNotice, text: "Plan approved.", mark: ChatMark.MarkPlanApproved });
  expect(afterSetup([interview, approved, item("after")]).map((x) => x.id)).toEqual(["after"]);
  expect(afterSetup([item("1")]).map((x) => x.id)).toEqual(["1"]);
});
