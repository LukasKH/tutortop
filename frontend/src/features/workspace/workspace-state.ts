import { CheckKind, type ChatItem, type Questions, type SandboxCheck, type WorkspaceState } from "@/lib/api";
import { objectiveOfWriter } from "@/lib/workspace";

/**
 * Everything the workspace screen knows from the backend. The snapshot
 * (App.Workspace) is refetched whenever something changes; the rest is kept
 * current by events in between, because those change too often or too fast
 * to wait for a refetch.
 */
export type LiveWorkspace = {
  ws: WorkspaceState | null;
  /** Which threads' tutor or writer is working, by thread name. */
  busy: Record<string, boolean | undefined>;
  /** Writers' sandbox checks of new exercises, by objective; they show on the objective. */
  checks: Record<string, SandboxCheck | undefined>;
  /** Why an objective's writer stopped, if it failed. Not in the snapshot. */
  writeFailed: Record<string, string | undefined>;
  /** A chat's sandbox check of a language it sets up (set_toolchain), shown in the chat's status line. Not in the snapshot. */
  toolchainCheck: SandboxCheck | null;
  /** The tutor's open questions (ask_user). */
  questions: Questions | null;
  /** The last failed call, shown above the centre pane until the next snapshot loads. */
  error: string;
  // When an event last set each busy flag and check, and when the snapshot
  // in use was asked for (performance.now()). A snapshot asked for before an
  // event is older than it, so it doesn't undo it: otherwise a writer that
  // finishes during a refetch would spin forever.
  busyAt: Record<string, number>;
  checkAt: Record<string, number>;
  snapshotAt: number;
};

/** What changes it: a snapshot arriving, one of the backend's events, or the learner. */
export type Action =
  | { type: "snapshot"; ws: WorkspaceState; asked: number }
  | { type: "failed"; error: string }
  | { type: "busy"; thread: string; busy: boolean; at: number }
  | { type: "check"; check: SandboxCheck; at: number }
  | { type: "chat"; item: ChatItem }
  | { type: "questions"; questions: Questions | null }
  | { type: "dismissFinishedCheck" };

export const initialWorkspace: LiveWorkspace = {
  ws: null,
  busy: {},
  checks: {},
  writeFailed: {},
  toolchainCheck: null,
  questions: null,
  error: "",
  busyAt: {},
  checkAt: {},
  snapshotAt: -Infinity,
};

export function reduceWorkspace(s: LiveWorkspace, a: Action): LiveWorkspace {
  switch (a.type) {
    case "snapshot":
      // Refetches can overlap during a burst of events; an older one landing late is dropped.
      if (a.asked < s.snapshotAt) return s;
      return {
        ...s,
        ws: a.ws,
        busy: newer(a.ws.busy ?? {}, s.busy, s.busyAt, a.asked),
        checks: newer(a.ws.checks ?? {}, s.checks, s.checkAt, a.asked),
        questions: a.ws.questions ?? null,
        error: "",
        snapshotAt: a.asked,
      };

    case "failed":
      return { ...s, error: a.error };

    case "busy": {
      const next = { ...s, busy: { ...s.busy, [a.thread]: a.busy }, busyAt: { ...s.busyAt, [a.thread]: a.at } };
      // A writer starting or finishing starts its objective's status afresh.
      const writing = objectiveOfWriter(a.thread);
      if (writing) {
        next.checks = { ...s.checks, [writing]: undefined };
        next.checkAt = { ...s.checkAt, [writing]: a.at };
        if (a.busy) next.writeFailed = { ...s.writeFailed, [writing]: undefined };
      }
      // A finished check is only news while the chat that ran it is still at work.
      const check = s.toolchainCheck;
      if (!a.busy && check?.thread === a.thread && !check.running) next.toolchainCheck = null;
      return next;
    }

    case "check": {
      if (a.check.kind === CheckKind.CheckToolchain) return { ...s, toolchainCheck: a.check };
      // A task is checked by its objective's writer, and shows on the objective.
      const objective = objectiveOfWriter(a.check.thread);
      if (!objective) return s;
      return { ...s, checks: { ...s.checks, [objective]: a.check }, checkAt: { ...s.checkAt, [objective]: a.at } };
    }

    case "chat": {
      // Writers aren't chats; only their failures matter here.
      const writing = objectiveOfWriter(a.item.thread);
      if (!writing || a.item.status !== "failed") return s;
      return { ...s, writeFailed: { ...s.writeFailed, [writing]: a.item.text } };
    }

    case "questions":
      return { ...s, questions: a.questions };

    case "dismissFinishedCheck":
      return s.toolchainCheck && !s.toolchainCheck.running ? { ...s, toolchainCheck: null } : s;
  }
}

/** Objectives whose lesson and exercise are being written in the background. */
export function writingObjectives(busy: LiveWorkspace["busy"]): Set<string> {
  return new Set(Object.keys(busy).flatMap((t) => (busy[t] ? (objectiveOfWriter(t) ?? []) : [])));
}

/** A tutor (not a writer) is working in some thread. */
export const tutorBusy = (busy: LiveWorkspace["busy"]) => Object.keys(busy).some((t) => busy[t] && !objectiveOfWriter(t));

/** The snapshot's values, except those events set after it was asked for. */
function newer<T>(snapshot: Record<string, T>, current: Record<string, T>, changedAt: Record<string, number>, asked: number): Record<string, T> {
  const next = { ...snapshot };
  for (const [key, at] of Object.entries(changedAt)) if (at > asked) next[key] = current[key];
  return next;
}
