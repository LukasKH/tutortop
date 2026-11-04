import { describe, expect, test } from "vitest";
import { ChatRole, type ChatItem, type Gate } from "@/lib/api";
import { initialWorkspace, reduceWorkspace, writingObjectives, type Action, type LiveWorkspace } from "@/features/workspace/workspace-state";
import { workspace } from "@/features/workspace/fixtures";

const run = (...actions: Action[]): LiveWorkspace => actions.reduce(reduceWorkspace, initialWorkspace);

const check = (objective: string, running: boolean): Gate => ({ objective, title: "Task", running, passed: false });

describe("snapshots and events", () => {
  test("a writer that finishes during a refetch doesn't spin forever", () => {
    const s = run(
      { type: "busy", thread: "write-a", busy: true, at: 1 },
      // Asked for at 2, while the writer was still busy; it finished at 3; the answer lands after.
      { type: "busy", thread: "write-a", busy: false, at: 3 },
      { type: "snapshot", ws: workspace({ busy: { "write-a": true } }), asked: 2 },
    );
    expect(s.busy["write-a"]).toBe(false);
    expect(writingObjectives(s.busy).size).toBe(0);
  });

  test("a snapshot asked for after an event is newer and wins", () => {
    const s = run({ type: "busy", thread: "plan", busy: true, at: 1 }, { type: "snapshot", ws: workspace({ busy: {} }), asked: 2 });
    expect(s.busy.plan).toBeUndefined();
  });

  test("a writer's check from an event survives an older snapshot", () => {
    const s = run({ type: "gate", gate: check("a", true), at: 3 }, { type: "snapshot", ws: workspace({ checks: {} }), asked: 2 });
    expect(s.checks.a?.running).toBe(true);
  });

  test("an older refetch landing after a newer one is dropped", () => {
    const s = run({ type: "snapshot", ws: workspace({ title: "new" }), asked: 5 }, { type: "snapshot", ws: workspace({ title: "old" }), asked: 4 });
    expect(s.ws?.title).toBe("new");
  });

  test("a snapshot clears the last error", () => {
    const s = run({ type: "failed", error: "Can't stop" }, { type: "snapshot", ws: workspace(), asked: 1 });
    expect(s.error).toBe("");
  });
});

describe("writers", () => {
  const failed: ChatItem = { id: "1", thread: "write-a", role: ChatRole.RoleNotice, text: "Agent crashed", status: "failed", at: "" };

  test("a failed writer is remembered until it starts again, and its check starts afresh", () => {
    let s = run({ type: "gate", gate: check("a", false), at: 1 }, { type: "chat", item: failed });
    expect(s.writeFailed.a).toBe("Agent crashed");
    s = reduceWorkspace(s, { type: "busy", thread: "write-a", busy: true, at: 2 });
    expect(s.writeFailed.a).toBeUndefined();
    expect(s.checks.a).toBeUndefined();
  });

  test("other chat messages don't count as writer failures", () => {
    const s = run({ type: "chat", item: { ...failed, thread: "plan" } }, { type: "chat", item: { ...failed, status: "done" } });
    expect(s.writeFailed).toEqual({});
  });
});

describe("the planning tutor's sandbox check", () => {
  const toolchain = (running: boolean): Gate => ({ kind: "toolchain", title: "Go", running, passed: !running });

  test("a finished check clears once the plan thread stops; a running one stays", () => {
    expect(run({ type: "gate", gate: toolchain(false), at: 1 }, { type: "busy", thread: "plan", busy: false, at: 2 }).gate).toBeNull();
    expect(run({ type: "gate", gate: toolchain(true), at: 1 }, { type: "busy", thread: "plan", busy: false, at: 2 }).gate?.running).toBe(true);
  });

  test("dismissing only clears a finished check", () => {
    expect(run({ type: "gate", gate: toolchain(true), at: 1 }, { type: "dismissFinishedGate" }).gate).not.toBeNull();
    expect(run({ type: "gate", gate: toolchain(false), at: 1 }, { type: "dismissFinishedGate" }).gate).toBeNull();
  });
});
