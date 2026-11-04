// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { AppEvents, type Questions, type WorkspaceState } from "@/lib/api";
import { centerOf, useOpenItem } from "@/features/workspace/use-open-item";
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
function render(ws: WorkspaceState, stored?: string) {
  if (stored) localStorage.setItem("tutortop.open-item.ws", JSON.stringify(stored));
  const dismissFinishedGate = vi.fn();
  const refresh = vi.fn(async () => ws);
  const hook = renderHook((p) => useOpenItem("ws", { ws: p.ws, refresh, dismissFinishedGate }), { initialProps: { ws } });
  return { ...hook, dismissFinishedGate, refresh };
}

describe("published work", () => {
  test("opens at once when its page was open, waiting for it, even from the plan page", () => {
    const { result, rerender, dismissFinishedGate } = render(workspace(), "todo:exercise:a");
    act(() => result.current.setView("plan"));
    emit(AppEvents.TaskPublished, { task: task("t1", "a") });
    rerender({ ws: workspace({ tasks: [task("t1", "a")] }) });
    expect(result.current.view).toBe("item");
    expect(result.current.item).toEqual({ kind: "task", id: "t1" });
    expect(dismissFinishedGate).toHaveBeenCalled();
  });

  test("opens at once when nothing is open", () => {
    const { result } = render(workspace());
    act(() => result.current.setView("plan"));
    emit(AppEvents.LessonPublished, { lesson: lesson("l1", "a") });
    expect(result.current.view).toBe("item");
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
    refresh.mockResolvedValue(proposed);
    await emit(AppEvents.CurriculumProposed, {});
    expect(result.current.view).toBe("proposal");
  });
});

test("the chat follows what's open: its own thread, or the plan's", () => {
  const { result } = render(workspace({ lessons: [lesson("l1", "a")] }), "lesson-l1");
  expect(result.current.thread).toBe("lesson-l1");
  act(() => result.current.setView("plan"));
  expect(result.current.thread).toBe("plan");
  act(() => result.current.open({ kind: "todo", objective: "b", part: "lesson" }));
  expect(result.current.thread).toBe("plan");
});

describe("the open item from an older version", () => {
  // By default the first lesson would open, ahead of its task.
  const ws = workspace({ lessons: [lesson("l1", "a"), lesson("l2", "b")], tasks: [task("t1", "a")] });

  test("stays open, and is stored under the new key", () => {
    localStorage.setItem("tutortop.task.ws", JSON.stringify("t1"));
    const { result } = render(ws);
    expect(result.current.item).toEqual({ kind: "task", id: "t1" });
    expect(localStorage.getItem("tutortop.open-item.ws")).toBe(JSON.stringify("t1"));
  });

  test("gives way to one stored since", () => {
    localStorage.setItem("tutortop.task.ws", JSON.stringify("t1"));
    const { result } = render(ws, "lesson-l2");
    expect(result.current.item).toEqual({ kind: "lesson", id: "l2" });
  });
});

describe("centerOf", () => {
  const questions: Questions = { thread: "plan", questions: [{ id: "q", question: "Why?" }] };
  const setup = workspace({ curriculum: null });

  test("before the first plan: the setup page, then the proposal, or the plan page if chosen", () => {
    expect(centerOf(setup, "item", null, questions).kind).toBe("setup");
    expect(centerOf({ ...setup, proposal: workspace().curriculum }, "item", null, null).kind).toBe("proposal");
    expect(centerOf(setup, "plan", null, null).kind).toBe("plan");
  });

  test("questions take the centre only while nothing is open", () => {
    expect(centerOf(workspace(), "item", null, questions).kind).toBe("questions");
    expect(centerOf(workspace({ tasks: [task("t1", "a")] }), "item", { kind: "task", id: "t1" }, questions).kind).toBe("task");
  });

  test("the open item, or the page that has it written", () => {
    const ws = workspace({ lessons: [lesson("l1", "a")] });
    expect(centerOf(ws, "item", { kind: "lesson", id: "l1" }, null).kind).toBe("lesson");
    expect(centerOf(ws, "item", { kind: "todo", objective: "b", part: "exercise" }, null)).toMatchObject({ kind: "not-written", part: "exercise" });
    expect(centerOf(ws, "item", null, null).kind).toBe("empty");
  });
});
