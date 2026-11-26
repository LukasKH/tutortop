// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { FeedbackChip, ObjectiveStatus, type WorkspaceSnapshot } from "@/lib/api";
import { HowWasThis } from "@/features/feedback/how-was-this";
import { LiveWorkspaceContext, type LiveWorkspace } from "@/features/workspace/use-workspace";
import { lesson, task, workspace } from "@/features/workspace/fixtures";

afterEach(() => {
  cleanup();
  localStorage.clear();
});

function show(opts: { known?: string; ws?: Partial<WorkspaceSnapshot>; compact?: boolean; keepLessonId?: string } = {}) {
  const send = vi.fn(async () => {});
  const setView = vi.fn();
  const snap = workspace({ known: opts.known ?? "Python", ...opts.ws });
  const live = { id: "ws", live: { ws: snap }, nav: { setView }, threads: {}, lang: "go" } as unknown as LiveWorkspace;
  const ui = (
    <LiveWorkspaceContext value={live}>
      <HowWasThis item="task:0001-sum" compact={opts.compact} keepLessonId={opts.keepLessonId} send={send} />
    </LiveWorkspaceContext>
  );
  return { send, setView, ...render(ui), ui };
}

const btn = (name: string) => screen.getByRole("button", { name });

describe("HowWasThis", () => {
  test("opens a dialog with default picks and details, sends them, and closes", async () => {
    const { send } = show();
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(btn("Submit feedback"));
    expect(screen.getByRole("dialog")).toBeTruthy();

    expect((btn("Send feedback") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(btn("Clear"));
    fireEvent.click(btn("Too technical")); // replaces Clear
    fireEvent.click(btn("More comparisons to Python"));
    fireEvent.change(screen.getByLabelText(/Details/), { target: { value: "  Shorter statements  " } });
    fireEvent.click(btn("Send feedback"));

    await screen.findByText(/your tutor will adjust future lessons/);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(send).toHaveBeenCalledExactlyOnceWith({
      chips: [FeedbackChip.FeedbackTooTechnical, FeedbackChip.FeedbackMoreComparisons],
      text: "Shorter statements",
      regenerate: false,
    });
  });

  test("Ctrl+Enter in details sends free-text feedback alone", async () => {
    const { send } = show({ known: "" });
    fireEvent.click(btn("Submit feedback"));
    expect(btn("More comparisons to what I know")).toBeTruthy();
    const field = screen.getByLabelText(/Details/);
    fireEvent.change(field, { target: { value: "More diagrams" } });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(send).toHaveBeenCalledWith({ chips: [], text: "More diagrams", regenerate: false }));
  });

  test("offers regenerating unsolved objectives when already-generated ones exist", async () => {
    const { send } = show({
      ws: {
        progress: {
          passed: 1,
          total: 3,
          passedEver: 1,
          objectives: {
            a: { id: "a", status: ObjectiveStatus.ObjectivePassed, tasks: ["0001-a"] },
            b: { id: "b", status: ObjectiveStatus.ObjectiveInProgress, tasks: ["0002-b"] },
            c: { id: "c", status: ObjectiveStatus.ObjectiveInProgress, tasks: ["0003-c"] },
          },
        },
        lessons: [lesson("0001-a", "a"), lesson("0002-b", "b"), lesson("0003-c", "c")],
        tasks: [task("0001-a", "a", true), task("0002-b", "b"), task("0003-c", "c")],
      },
    });
    fireEvent.click(btn("Submit feedback"));
    expect(screen.getByText(/Redo 2 already-generated objectives/)).toBeTruthy();
    fireEvent.click(btn("Too technical"));
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(btn("Send feedback"));

    await waitFor(() =>
      expect(send).toHaveBeenCalledExactlyOnceWith({
        chips: [FeedbackChip.FeedbackTooTechnical],
        text: "",
        regenerate: true,
      }),
    );
  });

  test("hides the regenerate option when no unsolved objectives are written yet", () => {
    show();
    fireEvent.click(btn("Submit feedback"));
    expect(screen.queryByRole("checkbox")).toBeNull();
  });

  test("once sent, compact mode shows a Feedback sent button that opens preferences", async () => {
    const first = show({ compact: true });
    fireEvent.click(btn("Submit feedback"));
    fireEvent.click(btn("Too long"));
    fireEvent.click(btn("Send feedback"));
    await screen.findByRole("button", { name: "Feedback sent" });
    first.unmount();

    const again = show({ compact: true });
    expect(screen.queryByRole("button", { name: "Submit feedback" })).toBeNull();
    fireEvent.click(btn("Feedback sent"));
    expect(again.setView).toHaveBeenCalledWith("plan");
  });

  test("a failed send keeps the dialog open with the learner's picks", async () => {
    const { send } = show();
    send.mockRejectedValueOnce(new Error("too many messages are waiting"));
    fireEvent.click(btn("Submit feedback"));
    fireEvent.click(btn("Too hard"));
    fireEvent.click(btn("Send feedback"));
    await screen.findByText("Too many messages are waiting");
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(btn("Too hard").getAttribute("aria-pressed")).toBe("true");
  });
});
