import { describe, expect, test } from "vitest";
import { describeItem, entryOf, exerciseOf, itemKey, parseItemKey, resolveOpen, type OpenItem } from "@/features/workspace/open-item";
import { lesson, task, workspace } from "@/features/workspace/fixtures";

describe("stored keys", () => {
  test.each<[string, OpenItem]>([
    ["0001-reverse", { kind: "task", id: "0001-reverse" }],
    ["lesson-0001-errors", { kind: "lesson", id: "0001-errors" }],
    ["todo:exercise:a", { kind: "todo", part: "exercise", objective: "a" }],
  ])("%s reads back as it was stored before OpenItem", (key, item) => {
    expect(parseItemKey(key)).toEqual(item);
    expect(itemKey(item)).toBe(key);
  });
});

describe("resolveOpen", () => {
  test("a page waiting for its lesson becomes the lesson once it's written", () => {
    const ws = workspace({ lessons: [lesson("l1", "a")] });
    expect(resolveOpen(ws, { kind: "todo", objective: "a", part: "lesson" })).toEqual({ kind: "lesson", id: "l1" });
  });

  test("a page waiting for its exercise stays open while the objective is in the plan", () => {
    const item: OpenItem = { kind: "todo", objective: "b", part: "exercise" };
    expect(resolveOpen(workspace(), item)).toBe(item);
    const replanned = workspace({ curriculum: { language: "go", modules: [] }, tasks: [task("t1", "x")] });
    expect(resolveOpen(replanned, item)).toEqual({ kind: "task", id: "t1" });
  });

  test("a deleted task falls back to the first unsolved task, through its unfinished lesson", () => {
    const ws = workspace({ tasks: [task("t1", "a", true), task("t2", "b")], lessons: [lesson("l2", "b", 1)] });
    expect(resolveOpen(ws, { kind: "task", id: "gone" })).toEqual({ kind: "lesson", id: "l2" });
    expect(resolveOpen(ws, null)).toEqual({ kind: "lesson", id: "l2" });
  });

  test("with every task solved, a lesson without an exercise comes first, else the last task", () => {
    expect(resolveOpen(workspace({ tasks: [task("t1", "a", true)], lessons: [lesson("l2", "b")] }), null)).toEqual({ kind: "lesson", id: "l2" });
    expect(resolveOpen(workspace({ tasks: [task("t1", "a", true), task("t2", "b", true)] }), null)).toEqual({ kind: "task", id: "t2" });
    expect(resolveOpen(workspace(), null)).toBeNull();
  });
});

test("an objective's exercise is its newest unsolved task, else its last", () => {
  expect(exerciseOf(workspace({ tasks: [task("t1", "a"), task("t2", "a"), task("t3", "a", true)] }), "a")).toEqual({ kind: "task", id: "t2" });
  expect(exerciseOf(workspace({ tasks: [task("t1", "a", true), task("t2", "a", true)] }), "a")).toEqual({ kind: "task", id: "t2" });
  expect(exerciseOf(workspace(), "a")).toEqual({ kind: "todo", objective: "a", part: "exercise" });
});

describe("entryOf: clicking an objective", () => {
  test("opens its lesson until the quiz is done, then its exercise", () => {
    const tasks = [task("t1", "a")];
    expect(entryOf(workspace({ tasks, lessons: [lesson("l1", "a", 1)] }), "a", null)).toEqual({ kind: "lesson", id: "l1" });
    expect(entryOf(workspace({ tasks, lessons: [lesson("l1", "a", 2)] }), "a", null)).toEqual({ kind: "task", id: "t1" });
  });

  test("keeps the objective's open exercise open", () => {
    const ws = workspace({ tasks: [task("t1", "a"), task("t2", "a")] });
    expect(entryOf(ws, "a", { kind: "task", id: "t1" })).toEqual({ kind: "task", id: "t1" });
  });

  test("with nothing written, opens the page that writes the lesson", () => {
    expect(entryOf(workspace(), "c", null)).toEqual({ kind: "todo", objective: "c", part: "lesson" });
  });
});

test("the next task prefers unsolved ones after the open task", () => {
  const ws = workspace({ tasks: [task("t1", "a"), task("t2", "b"), task("t3", "c")] });
  expect(describeItem(ws, { kind: "task", id: "t2" }).nextTask?.id).toBe("t3");
  expect(describeItem(ws, { kind: "task", id: "t3" }).nextTask?.id).toBe("t1");
});
