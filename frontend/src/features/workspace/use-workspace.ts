import { useCallback, useEffect, useRef, useState } from "react";
import { App, AppEvents, errorText, useEvent, type Gate, type QuestionForm, type WorkspaceState } from "@/lib/api";
import { objectiveOfWriter, PLAN_THREAD } from "@/lib/workspace";

/**
 * A workspace as the backend reports it, kept current by its events: the
 * snapshot, which threads are busy, writers' sandbox checks and failures, the
 * planning tutor's sandbox check, and the tutor's open questions.
 */
export function useWorkspace(id: string) {
  const [ws, setWs] = useState<WorkspaceState | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<Record<string, boolean | undefined>>({});
  // Writers' checks of new exercises, by objective; they show on the objective.
  const [checks, setChecks] = useState<Record<string, Gate | undefined>>({});
  // Why an objective's writer stopped, if it failed.
  const [writeFailed, setWriteFailed] = useState<Record<string, string | undefined>>({});
  // The planning tutor's sandbox check (a new language), shown in the chat's status line.
  const [gate, setGate] = useState<Gate | null>(null);
  const [questions, setQuestions] = useState<QuestionForm | null>(null);
  const fail = useCallback((e: unknown) => setError(errorText(e)), []);

  // When each thread's busy flag and each objective's check last changed by
  // event. A snapshot requested before that is older, so it doesn't undo it:
  // otherwise a writer that finishes during a refetch would spin forever.
  const changedAt = useRef<Record<string, number>>({});
  const changed = (key: string) => (changedAt.current[key] = performance.now());

  // Refetches can overlap during a burst of events; only the newest one wins.
  const seq = useRef(0);
  const refresh = useCallback(async () => {
    const mine = ++seq.current;
    const asked = performance.now();
    // Keep what events said after the snapshot was asked for.
    const newer = <T>(snapshot: Record<string, T>, current: Record<string, T>, prefix: string) => {
      const next = { ...snapshot };
      for (const [key, at] of Object.entries(changedAt.current)) {
        if (at > asked && key.startsWith(prefix)) next[key.slice(prefix.length)] = current[key.slice(prefix.length)];
      }
      return next;
    };
    try {
      const s = await App.Workspace(id);
      if (mine !== seq.current) return null;
      setWs(s);
      setBusy((cur) => newer<boolean | undefined>(s.busy ?? {}, cur, "busy:"));
      setChecks((cur) => newer<Gate | undefined>(s.checks ?? {}, cur, "check:"));
      setQuestions(s.questions ?? null);
      setError("");
      return s;
    } catch (e) {
      fail(e);
      return null;
    }
  }, [id, fail]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Writers aren't chats; only their failures matter here.
  useEvent(AppEvents.ChatMessage, ({ workspace, item }) => {
    const writing = workspace === id && objectiveOfWriter(item.thread);
    if (writing && item.status === "failed") setWriteFailed((f) => ({ ...f, [writing]: item.text }));
  });

  useEvent(AppEvents.ThreadBusy, (b) => {
    if (b.workspace !== id) return;
    const t = b.thread || PLAN_THREAD;
    changed(`busy:${t}`);
    setBusy((all) => ({ ...all, [t]: b.busy }));
    // A writer starting or finishing starts its objective's status afresh.
    const writing = objectiveOfWriter(t);
    if (writing) {
      changed(`check:${writing}`);
      setChecks((c) => ({ ...c, [writing]: undefined }));
      if (b.busy) setWriteFailed((f) => ({ ...f, [writing]: undefined }));
    }
    // A finished gate result is only interesting while the plan thread works on it.
    if (t === PLAN_THREAD && !b.busy) setGate((g) => (g?.running ? g : null));
  });

  useEvent(AppEvents.WorkspaceChanged, ({ workspace }) => {
    if (workspace === id) refresh();
  });

  useEvent(AppEvents.QuestionsAsked, ({ workspace, questions }) => {
    if (workspace === id) setQuestions(questions);
  });

  useEvent(AppEvents.GateStatus, ({ workspace, gate: g }) => {
    if (workspace !== id) return;
    if (g.objective) {
      changed(`check:${g.objective}`);
      setChecks((c) => ({ ...c, [g.objective!]: g }));
    } else setGate(g);
  });

  async function answerQuestions(answers: Record<string, string>) {
    await App.AnswerQuestions(id, answers);
    setQuestions(null);
  }

  function dismissQuestions() {
    App.DismissQuestions(id).catch(fail);
    setQuestions(null);
  }

  /** Clears a finished planning check from the status line, e.g. once the learner moves on. */
  const dismissFinishedGate = () => setGate((g) => (g && !g.running ? null : g));

  return { ws, error, fail, refresh, busy, checks, writeFailed, gate, dismissFinishedGate, questions, answerQuestions, dismissQuestions };
}
