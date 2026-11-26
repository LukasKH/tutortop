// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { FeedbackChip } from "@/lib/api";
import { HowWasThis } from "@/features/feedback/how-was-this";
import { LiveWorkspaceContext, type LiveWorkspace } from "@/features/workspace/use-workspace";
import { workspace } from "@/features/workspace/fixtures";

afterEach(() => {
  cleanup();
  localStorage.clear();
});

/** Renders the line in a workspace whose mission says the learner knows known. */
function show(known = "Python") {
  const send = vi.fn(async () => {});
  const setView = vi.fn();
  const live = { id: "ws", live: { ws: workspace({ known }) }, nav: { setView }, threads: {}, lang: "go" } as unknown as LiveWorkspace;
  const ui = (
    <LiveWorkspaceContext value={live}>
      <HowWasThis item="task:0001-sum" send={send} />
    </LiveWorkspaceContext>
  );
  return { send, setView, ...render(ui), ui };
}

const chip = (name: string) => screen.getByRole("button", { name });

describe("HowWasThis", () => {
  test("sends the picked chips and the learner's words, then thanks them", async () => {
    const { send } = show();
    expect(screen.queryByRole("button", { name: "Send" })).toBeNull(); // nothing to send yet
    fireEvent.click(chip("Clear"));
    fireEvent.click(chip("Too technical")); // replaces Clear
    fireEvent.click(chip("More comparisons to Python"));
    fireEvent.click(chip("Say more…"));
    fireEvent.change(screen.getByRole("textbox", { name: "Say more" }), { target: { value: "  Shorter statements  " } });
    fireEvent.click(chip("Send"));

    await screen.findByText(/your tutor will adjust future lessons/);
    expect(send).toHaveBeenCalledExactlyOnceWith({
      chips: [FeedbackChip.FeedbackTooTechnical, FeedbackChip.FeedbackMoreComparisons],
      text: "Shorter statements",
    });
  });

  test("Enter in the text field sends it alone", async () => {
    const { send } = show("");
    expect(chip("More comparisons to what I know")).toBeTruthy();
    fireEvent.click(chip("Say more…"));
    const field = screen.getByRole("textbox", { name: "Say more" });
    fireEvent.change(field, { target: { value: "More diagrams" } });
    fireEvent.keyDown(field, { key: "Enter" });
    await waitFor(() => expect(send).toHaveBeenCalledWith({ chips: [], text: "More diagrams" }));
  });

  test("once sent, it thanks instead of asking again, and links to the preferences", async () => {
    const first = show();
    fireEvent.click(chip("Too long"));
    fireEvent.click(chip("Send"));
    await screen.findByText(/your tutor will adjust future lessons/);
    first.unmount();

    const again = show();
    expect(screen.queryByRole("group", { name: "How was this?" })).toBeNull();
    fireEvent.click(chip("See how you like to learn →"));
    expect(again.setView).toHaveBeenCalledWith("plan");
  });

  test("a failed send keeps the picks to try again", async () => {
    const { send } = show();
    send.mockRejectedValueOnce(new Error("too many messages are waiting"));
    fireEvent.click(chip("Too hard"));
    fireEvent.click(chip("Send"));
    await screen.findByText("Too many messages are waiting");
    expect(chip("Too hard").getAttribute("aria-pressed")).toBe("true");
  });
});
