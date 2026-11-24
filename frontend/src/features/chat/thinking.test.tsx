// @vitest-environment jsdom
import { afterEach, expect, test } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ChatRole, type ChatItem, type Working } from "@/lib/api";
import { Thinking } from "@/features/chat/thinking";

afterEach(cleanup);

const asked: ChatItem = { id: "q", thread: "plan", role: ChatRole.RoleUser, text: "Plan Go for me", at: "" };
const working = (over: Partial<Working>): Working => ({ workspace: "ws", thread: "plan", thinking: "", preparing: "", ...over });

test("without word from the agent, the tutor is just thinking", () => {
  render(<Thinking items={[asked]} busy />);
  expect(screen.getByRole("status").textContent).toBe("Thinking…");
});

test("the tutor's latest reasoning shows, and unfolds in full", () => {
  const { container } = render(<Thinking items={[asked]} busy working={working({ thinking: "**Reading notes**\n\nThe learner knows Python." })} />);
  expect(container.querySelector("details summary")?.textContent).toBe("The learner knows Python.");
  expect(container.querySelector("details p")?.textContent).toBe("**Reading notes**\n\nThe learner knows Python.");
  expect(container.querySelector("summary [role='status']")).toBeNull();
});

test("a long tool call being written shows what it is, even after a reply", () => {
  const reply: ChatItem = { ...asked, id: "r", role: ChatRole.RoleAssistant, text: "I'll draft your plan." };
  const { container } = render(<Thinking items={[asked, reply]} busy working={working({ thinking: "hmm", preparing: "Drafting your plan" })} />);
  expect(container.querySelector("details summary")?.textContent).toBe("Drafting your plan…");
});

test("whitespace-only thinking falls back to plain Thinking… without an empty details", () => {
  const { container } = render(<Thinking items={[asked]} busy working={working({ thinking: "  \n\n  " })} />);
  expect(screen.getByRole("status").textContent).toBe("Thinking…");
  expect(container.querySelector("details")).toBeNull();
});

test("nothing shows once the tutor is done", () => {
  const { container } = render(<Thinking items={[asked]} busy={false} working={working({ thinking: "hmm" })} />);
  expect(container.textContent).toBe("");
});
