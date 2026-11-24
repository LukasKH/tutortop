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
  test("a snapshot overrides older events, but a writer that finishes during the refetch doesn't spin forever", () => {
    const s = run(
      { type: "busy", thread: "plan", busy: true, seq: 1 },
      { type: "busy", thread: "write-a", busy: true, seq: 1 },
      // Read at 2, while the writer was still busy; it finished at 3; the snapshot lands after.
      { type: "busy", thread: "write-a", busy: false, seq: 3 },
      // Another writer's check started at 4, so the snapshot doesn't have it either.
      { type: "check", check: check("b", true), seq: 4 },
      { type: "snapshot", ws: workspace({ busy: { "write-a": true }, checks: {}, seq: 2 }) },
    );
    expect(s.busy["write-a"]).toBe(false);
    expect(writingObjectives(s).size).toBe(0);
    expect(s.checks.b?.running).toBe(true);
    expect(s.busy.plan).toBeUndefined();
  });

  test("an older refetch landing after a newer one is dropped", () => {
    const s = run({ type: "snapshot", ws: workspace({ title: "new", seq: 5 }) }, { type: "snapshot", ws: workspace({ title: "old", seq: 4 }) });
    expect(s.ws?.title).toBe("new");
  });
});

describe("writers", () => {
  const failed: ChatItem = { id: "1", thread: "write-a", role: ChatRole.RoleNotice, text: "Agent crashed", status: ChatStatus.ChatFailed, at: "" };

  test("a failed writer is remembered until it starts again, and its check starts afresh", () => {
    // A message that isn't a failure doesn't count, nor a failed tool call the writer recovers from.
    let s = run(
      { type: "check", check: check("a", false), seq: 1 },
      { type: "chat", item: { ...failed, status: ChatStatus.ChatDone } },
      { type: "chat", item: { ...failed, role: ChatRole.RoleTool, text: "curl.exe -sI https://go.dev" } },
    );
    expect(s.writeFailed.a).toBeUndefined();
    s = reduceWorkspace(s, { type: "chat", item: failed });
    expect(s.writeFailed.a).toBe("Agent crashed");
    s = reduceWorkspace(s, { type: "busy", thread: "write-a", busy: true, seq: 2 });
    expect(s.writeFailed.a).toBeUndefined();
    expect(s.checks.a).toBeUndefined();
  });
});

describe("a chat's toolchain check", () => {
  const toolchain = (thread: string, running: boolean): SandboxCheck => ({ kind: CheckKind.CheckToolchain, thread, title: "Rust", running, passed: !running });
  const busy = (thread: string, value: boolean): Action => ({ type: "busy", thread, busy: value, seq: value ? 1 : 3 });
  const checked = (c: SandboxCheck): Action => ({ type: "check", check: c, seq: 2 });

  test("a check clears once the thread that ran it stops, even if still running; another thread stopping leaves it", () => {
    expect(run(busy("plan", true), checked(toolchain("plan", false)), busy("plan", false)).toolchainCheck).toBeNull();
    expect(run(busy("plan", true), checked(toolchain("plan", true)), busy("plan", false)).toolchainCheck).toBeNull();
    expect(run(busy("lesson-l1", true), checked(toolchain("lesson-l1", false)), busy("plan", false)).toolchainCheck?.thread).toBe("lesson-l1");
    expect(run(busy("plan", true), checked(toolchain("plan", true)), busy("plan", false), checked(toolchain("plan", false))).toolchainCheck).toBeNull();
  });
});
