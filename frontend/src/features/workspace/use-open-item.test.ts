// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { AppEvents, type Questions, type WorkspaceSnapshot } from "@/lib/api";
import { centerOf, placeQuestions, useOpenItem, type CenterView, type QuestionsPlace } from "@/features/workspace/use-open-item";
import { describeItem, type OpenItem } from "@/features/workspace/open-item";
import { lesson, task, workspace } from "@/features/workspace/fixtures";

// Events come from this fake instead of the Wails runtime: emit() calls what the hook listens with.
const { handlers } = vi.hoisted(() => ({ handlers: new Map<string, (data: unknown) => void>() }));
vi.mock("@/lib/api", async (original) => ({
  ...(await original<typeof import("@/lib/api")>()),
  useEvent: (name: string, handler: (data: unknown) => void) => handlers.set(name, handler),
}));
const emit = (name: string, data: object) => act(() => handlers.get(name)!({ workspace: "ws", ...data }));

afterEach(() => {
  cleanup();
  localStorage.clear();
});

/** Renders the hook on a snapshot, with the item stored from an earlier session. rerender({ ws }) passes a newer snapshot. */
function render(ws: WorkspaceSnapshot, stored?: string) {
  if (stored) localStorage.setItem("tutortop.open-item.ws", JSON.stringify(stored));
  const refresh = vi.fn(async () => ws);
  const hook = renderHook((p) => useOpenItem("ws", { ws: p.ws, refresh, dismissFinishedCheck: () => {} }), { initialProps: { ws } });
  return { ...hook, refresh };
}

describe("published work", () => {
  test.each([
    {
      when: "its page was open, waiting for it",
      stored: "todo:exercise:a",
      event: AppEvents.TaskPublished,
      data: { task: task("t1", "a") },
      after: workspace({ tasks: [task("t1", "a")] }),
      opened: { kind: "task", id: "t1" },
    },
    {
      when: "nothing was open",
      stored: undefined,
      event: AppEvents.LessonPublished,
      data: { lesson: lesson("l1", "a") },
      after: workspace({ lessons: [lesson("l1", "a")] }),
      opened: { kind: "lesson", id: "l1" },
    },
  ])("opens at once when $when, even from the plan page", ({ stored, event, data, after, opened }) => {
    const { result, rerender } = render(workspace(), stored);
    act(() => result.current.setView("plan"));
    emit(event, data);
    rerender({ ws: after });
    expect(result.current.view).toBe("item");
    expect(result.current.item).toEqual(opened);
  });

  test("doesn't interrupt an unsolved task", () => {
    const { result, rerender } = render(workspace({ tasks: [task("t1", "a")] }), "t1");
    act(() => result.current.setView("plan"));
    emit(AppEvents.TaskPublished, { task: task("t2", "b") });
    rerender({ ws: workspace({ tasks: [task("t1", "a"), task("t2", "b")] }) });
    expect(result.current.view).toBe("plan");
    expect(result.current.item).toEqual({ kind: "task", id: "t1" });
    expect(result.current.newItem).toBeNull();
  });

  test("is pointed out once the open task is solved, an announced lesson first", () => {
    const { result } = render(workspace({ tasks: [task("t1", "a", true)] }), "t1");
    emit(AppEvents.LessonPublished, { lesson: lesson("l2", "b") });
    emit(AppEvents.TaskPublished, { task: task("t2", "b") });
    expect(result.current.item).toEqual({ kind: "task", id: "t1" });
    expect(result.current.newItem).toEqual({ item: { kind: "lesson", id: "l2" }, title: "Lesson l2" });
    act(() => result.current.open({ kind: "lesson", id: "l2" }));
    expect(result.current.newItem).toBeNull();
  });

  test("a proposal opens for review once it's loaded", async () => {
    const proposed = workspace({ proposal: workspace().curriculum });
    const { result, refresh } = render(workspace(), "t1");
    act(() => result.current.setView("plan"));
    refresh.mockResolvedValue(proposed);
    await emit(AppEvents.CurriculumProposed, {});
    expect(result.current.view).toBe("proposal");
  });
});

describe("placeQuestions", () => {
  const asked = (thread: string): Questions => ({
    thread,
    questions: [
      { id: "a", question: "A?" },
      { id: "b", question: "B?" },
    ],
  });
  const ws = workspace({ tasks: [task("t1", "a")] });
  const setup = workspace({ curriculum: null });
  const proposed = { ...setup, proposal: workspace().curriculum };
  const t1: OpenItem = { kind: "task", id: "t1" };

  test.each<[string, WorkspaceSnapshot, CenterView, OpenItem | null, string, string, QuestionsPlace]>([
    ["during setup: the setup page", setup, "item", t1, "plan", "plan", { center: true, chat: null, planBadge: 0 }],
    ["during setup with the plan open: not the centre", setup, "plan", null, "plan", "plan", { center: false, chat: null, planBadge: 0 }],
    ["during setup with a proposal open: not the centre", proposed, "item", null, "plan", "plan", { center: false, chat: null, planBadge: 0 }],
    ["while nothing is open: the centre, never a chat", ws, "item", null, "plan", "plan", { center: true, chat: null, planBadge: 0 }],
    ["with something open: the chat of the thread that asked", ws, "item", t1, "t1", "t1", { center: false, chat: "t1", planBadge: 0 }],
    ["with a plan page open: the plan's own chat is shown, so no badge", ws, "plan", null, "plan", "plan", { center: false, chat: "plan", planBadge: 0 }],
    ["the plan's, while another chat is shown: counted on Your plan", ws, "item", t1, "t1", "plan", { center: false, chat: "plan", planBadge: 2 }],
  ])("%s", (_, snapshot, view, item, thread, asker, place) => {
    expect(placeQuestions(snapshot, view, item, thread, asked(asker))).toEqual(place);
  });
});

describe("centerOf", () => {
  const questions: Questions = { thread: "plan", questions: [{ id: "q", question: "Why?" }] };
  const setup = workspace({ curriculum: null });
  const center = (ws: WorkspaceSnapshot, view: CenterView, item: OpenItem | null, centred: Questions | null) =>
    centerOf(ws, view, describeItem(ws, item), centred);

  test("before the first plan: the setup page, then the proposal, or the plan page if chosen", () => {
    expect(center(setup, "item", null, questions).kind).toBe("setup");
    expect(center({ ...setup, proposal: workspace().curriculum, resources: "# Resources" }, "item", null, null)).toMatchObject({
      kind: "proposal",
      resources: "# Resources", // for its Sources tab
    });
    expect(center(setup, "plan", null, null)).toMatchObject({ kind: "plan", canGoBack: true, canAskTutor: false });
  });

  test("questions placed in the centre take it, unless a plan page is chosen", () => {
    expect(center(workspace(), "item", null, questions).kind).toBe("questions");
    expect(center(workspace(), "plan", null, questions)).toMatchObject({ kind: "plan", canGoBack: false, canAskTutor: true });
  });

  test("the open item, or the page that has it written, linking the other part only once it's written", () => {
    const ws = workspace({ lessons: [lesson("l1", "a")] });
    expect(center(ws, "item", { kind: "lesson", id: "l1" }, null).kind).toBe("lesson");
    expect(center(ws, "item", { kind: "todo", objective: "a", part: "exercise" }, null)).toMatchObject({ kind: "not-written", part: "exercise" });
    expect(center(ws, "item", { kind: "todo", objective: "c", part: "lesson" }, null)).toMatchObject({ kind: "not-written", other: null });
    expect(center(ws, "item", null, null).kind).toBe("empty");
  });

  test("Next task leads through its objective's unfinished lesson", () => {
    const ws = workspace({ lessons: [lesson("l2", "b")], tasks: [task("t1", "a"), task("t2", "b")] });
    expect(center(ws, "item", { kind: "task", id: "t1" }, null)).toMatchObject({ nextTask: { id: "t2" }, openNext: { kind: "lesson", id: "l2" } });
  });
});
