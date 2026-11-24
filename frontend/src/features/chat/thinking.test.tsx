// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ChatRole, type ChatItem, type Working } from "@/lib/api";
import { WorkingContext, WorkingStore } from "@/features/workspace/use-working";
import { Thinking } from "@/features/chat/thinking";

// Monaco needs a real browser; the markdown here only needs it for colouring code.
vi.mock("@/lib/monaco", () => ({ monaco: { languages: { getLanguages: () => [] }, editor: { colorize: async (code: string) => code } } }));

afterEach(cleanup);

const asked: ChatItem = { id: "q", thread: "plan", role: ChatRole.RoleUser, text: "Plan Go for me", at: "" };
const working = (over: Partial<Working>): Working => ({ workspace: "ws", thread: "plan", thinking: "", preparing: "", ...over });

function renderThinking(items: ChatItem[], busy: boolean, state?: Working) {
  const store = new WorkingStore();
  if (state) store.set("plan", state);
  return render(
    <WorkingContext value={store}>
      <Thinking thread="plan" items={items} busy={busy} />
    </WorkingContext>,
  );
}

test("without word from the agent, the tutor is just thinking", () => {
  renderThinking([asked], true);
  expect(screen.getByRole("status").textContent).toBe("Thinking…");
});

test("the tutor's latest reasoning shows, and unfolds as formatted Markdown", () => {
  const { container } = renderThinking([asked], true, working({ thinking: "**Reading notes**\n\nThe learner knows Python." }));
  expect(container.querySelector("details summary")?.textContent).toBe("The learner knows Python.");
  expect(container.querySelector("details strong")?.textContent).toBe("Reading notes");
  expect(container.querySelector("details .prose")?.textContent).not.toContain("**");
  expect(container.querySelector("summary [role='status']")).toBeNull();
});

test("a long tool call being written shows what it is, even after a reply", () => {
  const reply: ChatItem = { ...asked, id: "r", role: ChatRole.RoleAssistant, text: "I'll draft your plan." };
  const { container } = renderThinking([asked, reply], true, working({ thinking: "hmm", preparing: "Drafting your plan" }));
  expect(container.querySelector("details summary")?.textContent).toBe("Drafting your plan…");
});

test("whitespace-only thinking falls back to plain Thinking… without an empty details", () => {
  const { container } = renderThinking([asked], true, working({ thinking: "  \n\n  " }));
  expect(screen.getByRole("status").textContent).toBe("Thinking…");
  expect(container.querySelector("details")).toBeNull();
});

test("nothing shows once the tutor is done", () => {
  const { container } = renderThinking([asked], false, working({ thinking: "hmm" }));
  expect(container.textContent).toBe("");
});
