import { CheckKind, type ChatItem, type Questions, type SandboxCheck, type WorkspaceSnapshot } from "@/lib/api";
import { objectiveOfWriter } from "@/lib/workspace";

/**
 * Everything the workspace screen knows from the backend. The snapshot
 * (App.Workspace) is refetched whenever something changes; the rest is kept
 * current by events in between, because those change too often or too fast
 * to wait for a refetch.
 */
export type LiveWorkspace = {
  ws: WorkspaceSnapshot | null;
  /**
   * Which threads' tutor or writer is working, by thread name: the one source
   * of everything the screen draws as working. The snapshot's, overridden by
   * newer thread:busy events. Read it through isBusy, tutorBusy and
   * writingObjectives below.
   */
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
  // The backend numbers busy and check events and snapshots in the order they
  // happened (seq). These are the numbers of the events that last set each
  // busy flag and check, and of the snapshot in use. A snapshot read before
  // an event has a lower number, so it doesn't undo it: otherwise a writer
  // that finishes during a refetch would spin forever.
  busySeq: Record<string, number>;
  checkSeq: Record<string, number>;
  snapshotSeq: number;
};

/** What changes it: a snapshot arriving, one of the backend's events, or the learner. */
export type Action =
  | { type: "snapshot"; ws: WorkspaceSnapshot }
  | { type: "failed"; error: string }
  | { type: "busy"; thread: string; busy: boolean; seq: number }
  | { type: "check"; check: SandboxCheck; seq: number }
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
  busySeq: {},
  checkSeq: {},
  snapshotSeq: 0,
};

export function reduceWorkspace(s: LiveWorkspace, a: Action): LiveWorkspace {
  switch (a.type) {
    case "snapshot":
      // Refetches can overlap during a burst of events; an older one landing late is dropped.
      if (a.ws.seq < s.snapshotSeq) return s;
      return {
        ...s,
        ws: a.ws,
        busy: newer(a.ws.busy ?? {}, s.busy, s.busySeq, a.ws.seq),
        checks: newer(a.ws.checks ?? {}, s.checks, s.checkSeq, a.ws.seq),
        questions: a.ws.questions ?? null,
        error: "",
        snapshotSeq: a.ws.seq,
      };

    case "failed":
      return { ...s, error: a.error };

    case "busy": {
      const next = { ...s, busy: { ...s.busy, [a.thread]: a.busy }, busySeq: { ...s.busySeq, [a.thread]: a.seq } };
      // A writer starting or finishing starts its objective's status afresh.
      const writing = objectiveOfWriter(a.thread);
      if (writing) {
        next.checks = { ...s.checks, [writing]: undefined };
        next.checkSeq = { ...s.checkSeq, [writing]: a.seq };
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
      return { ...s, checks: { ...s.checks, [objective]: a.check }, checkSeq: { ...s.checkSeq, [objective]: a.seq } };
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

// Busy, as the screen draws it. useWorkspace binds these to its state.

/**
 * One thread's tutor or writer is working. For the shown chat: "Thinking…",
 * the Stop button and the disabled hint button; the setup page's work log;
 * the dot on "Your plan".
 */
export const isBusy = (s: LiveWorkspace, thread: string) => !!s.busy[thread];

/** A tutor (not a writer) is working in some chat: the spinner on the folded chat panel. */
export const tutorBusy = (s: LiveWorkspace) => Object.keys(s.busy).some((t) => s.busy[t] && !objectiveOfWriter(t));

/**
 * Objectives whose lesson and exercise are being written in the background:
 * the curriculum's spinners, a not-written page, and the empty page's title.
 */
export function writingObjectives(s: LiveWorkspace): Set<string> {
  return new Set(Object.keys(s.busy).flatMap((t) => (s.busy[t] ? (objectiveOfWriter(t) ?? []) : [])));
}

/** The snapshot's values, except those events set after it was read. */
function newer<T>(snapshot: Record<string, T>, current: Record<string, T>, changedSeq: Record<string, number>, snapshotSeq: number): Record<string, T> {
  const next = { ...snapshot };
  for (const [key, seq] of Object.entries(changedSeq)) if (seq > snapshotSeq) next[key] = current[key];
  return next;
}
