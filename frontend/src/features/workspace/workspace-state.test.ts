import { describe, expect, test } from "vitest";
import { ChatRole, ChatStatus, CheckKind, type ChatItem, type SandboxCheck } from "@/lib/api";
import { initialWorkspace, reduceWorkspace, writingObjectives, type Action, type LiveWorkspace } from "@/features/workspace/workspace-state";
import { workspace } from "@/features/workspace/fixtures";

const run = (...actions: Action[]): LiveWorkspace => actions.reduce(reduceWorkspace, initialWorkspace);

const check = (objective: string, running: boolean): SandboxCheck => ({
  kind: CheckKind.CheckTask,
  thread: `write-${objective}`,
  title: "Task",
  running,
  passed: false,
});

describe("snapshots and events", () => {
  test("a writer that finishes during a refetch doesn't spin forever", () => {
    const s = run(
      { type: "busy", thread: "write-a", busy: true, seq: 1 },
      // Read at 2, while the writer was still busy; it finished at 3; the snapshot lands after.
      { type: "busy", thread: "write-a", busy: false, seq: 3 },
      { type: "snapshot", ws: workspace({ busy: { "write-a": true }, seq: 2 }) },
    );
    expect(s.busy["write-a"]).toBe(false);
    expect(writingObjectives(s.busy).size).toBe(0);
  });

  test("a snapshot read after an event is newer and wins", () => {
    const s = run({ type: "busy", thread: "plan", busy: true, seq: 1 }, { type: "snapshot", ws: workspace({ busy: {}, seq: 2 }) });
    expect(s.busy.plan).toBeUndefined();
  });

  test("a writer's check from an event survives an older snapshot", () => {
    const s = run({ type: "check", check: check("a", true), seq: 3 }, { type: "snapshot", ws: workspace({ checks: {}, seq: 2 }) });
    expect(s.checks.a?.running).toBe(true);
  });

  test("an older refetch landing after a newer one is dropped", () => {
    const s = run({ type: "snapshot", ws: workspace({ title: "new", seq: 5 }) }, { type: "snapshot", ws: workspace({ title: "old", seq: 4 }) });
    expect(s.ws?.title).toBe("new");
  });

  test("a snapshot clears the last error", () => {
    const s = run({ type: "failed", error: "Can't stop" }, { type: "snapshot", ws: workspace() });
    expect(s.error).toBe("");
  });
});

describe("writers", () => {
  const failed: ChatItem = { id: "1", thread: "write-a", role: ChatRole.RoleNotice, text: "Agent crashed", status: ChatStatus.ChatFailed, at: "" };

  test("a failed writer is remembered until it starts again, and its check starts afresh", () => {
    let s = run({ type: "check", check: check("a", false), seq: 1 }, { type: "chat", item: failed });
    expect(s.writeFailed.a).toBe("Agent crashed");
    s = reduceWorkspace(s, { type: "busy", thread: "write-a", busy: true, seq: 2 });
    expect(s.writeFailed.a).toBeUndefined();
    expect(s.checks.a).toBeUndefined();
  });

  test("other chat messages don't count as writer failures", () => {
    const s = run({ type: "chat", item: { ...failed, thread: "plan" } }, { type: "chat", item: { ...failed, status: ChatStatus.ChatDone } });
    expect(s.writeFailed).toEqual({});
  });
});

describe("a chat's toolchain check", () => {
  const toolchain = (thread: string, running: boolean): SandboxCheck => ({ kind: CheckKind.CheckToolchain, thread, title: "Rust", running, passed: !running });
  const checked = (c: SandboxCheck): Action => ({ type: "check", check: c, seq: 1 });
  const idle = (thread: string): Action => ({ type: "busy", thread, busy: false, seq: 2 });

  test("a finished check clears once the thread that ran it stops; a running one stays", () => {
    expect(run(checked(toolchain("plan", false)), idle("plan")).toolchainCheck).toBeNull();
    expect(run(checked(toolchain("lesson-l1", false)), idle("lesson-l1")).toolchainCheck).toBeNull();
    expect(run(checked(toolchain("plan", true)), idle("plan")).toolchainCheck?.running).toBe(true);
  });

  test("another thread stopping leaves it", () => {
    expect(run(checked(toolchain("lesson-l1", false)), idle("plan")).toolchainCheck?.thread).toBe("lesson-l1");
  });

  test("isn't an objective's check", () => {
    expect(run(checked(toolchain("plan", true))).checks).toEqual({});
  });

  test("dismissing only clears a finished check", () => {
    expect(run(checked(toolchain("plan", true)), { type: "dismissFinishedCheck" }).toolchainCheck).not.toBeNull();
    expect(run(checked(toolchain("plan", false)), { type: "dismissFinishedCheck" }).toolchainCheck).toBeNull();
  });
});
