import { useEffect, useEffectEvent } from "react";
import { Events } from "@wailsio/runtime";
import { Tutortop } from "../bindings/events";
import { App as Bindings } from "../bindings/github.com/LukasKH/tutortop/internal/app";
import type { Environment, GateEvent, Settings, WorkspaceState } from "../bindings/github.com/LukasKH/tutortop/internal/app";
import type { Model as AgentModel } from "../bindings/github.com/LukasKH/tutortop/internal/agent";
import type {
  Activity,
  ChatItem,
  Curriculum,
  LessonDetail,
  LessonStatus,
  QuizResult,
  Progress,
  Question,
  Source,
  Questions as QuestionForm,
  Summary,
  TaskDetail,
  TaskStatus as TaskInfo,
} from "../bindings/github.com/LukasKH/tutortop/internal/workspace";
import type { Files, Result, TestResult } from "../bindings/github.com/LukasKH/tutortop/internal/runner";

export type {
  Activity,
  AgentModel,
  ChatItem,
  Curriculum,
  Environment,
  Files,
  GateEvent,
  LessonDetail,
  LessonStatus,
  Progress,
  Question,
  QuestionForm,
  QuizResult,
  Result,
  Source,
  Settings,
  Summary,
  TaskDetail,
  TaskInfo,
  TestResult,
  WorkspaceState,
};

// ---- events ----

/** What each kind of the backend's "tutortop" event carries (App.emit in internal/app). */
type EventData = {
  chat: ChatItem;
  busy: { thread: string; busy: boolean };
  gate: GateEvent;
  // workspace.TaskInfo and LessonInfo: the status without the learner's progress.
  task: Omit<TaskInfo, "passed" | "hintLevel">;
  lesson: Omit<LessonStatus, "answered" | "correct">;
  questions: QuestionForm;
  // A message from a language server, or (closed) its end.
  lsp: { id: string; closed?: false; message: string } | { id: string; closed: true };
  changed: undefined;
  proposal: undefined;
};

export type AppEvent = { [K in keyof EventData]: { workspace: string; kind: K; data: EventData[K] } }[keyof EventData];

/** Subscribes to the backend's single "tutortop" event. Returns the unsubscribe function. */
export function onAppEvent(handler: (ev: AppEvent) => void): () => void {
  return Events.On(Tutortop, (ev) => handler(ev.data as AppEvent));
}

/** onAppEvent for the component's lifetime. */
export function useAppEvents(handler: (ev: AppEvent) => void) {
  const onEvent = useEffectEvent(handler);
  useEffect(() => onAppEvent((ev) => onEvent(ev)), []);
}

// ---- helpers ----

/**
 * The open item in a workspace is a task id or a lesson's key. A lesson's key
 * is also its chat thread, as the backend names it (workspace.LessonThread).
 */
export const lessonKey = (lessonId: string) => `lesson-${lessonId}`;
export const lessonOfKey = (key: string | null) => (key?.startsWith("lesson-") ? key.slice("lesson-".length) : null);

/**
 * The background conversation that writes an objective's lesson and exercise,
 * as the backend names it (workspace.WriterThread). It isn't shown as a chat;
 * while it's busy, the objective shows as being written.
 */
export const writerThread = (objective: string) => `write-${objective}`;
export const objectiveOfWriter = (thread: string) => (thread.startsWith("write-") ? thread.slice("write-".length) : null);

/**
 * An objective's lesson or exercise that hasn't been written yet. Opening
 * one shows a page that can have it written now. Not a chat thread.
 */
export type Part = "lesson" | "exercise";
export const todoKey = (objective: string, part: Part) => `todo:${part}:${objective}`;
export function todoOfKey(key: string | null): { objective: string; part: Part } | null {
  const m = key?.match(/^todo:(lesson|exercise):(.+)$/);
  return m ? { part: m[1] as Part, objective: m[2] } : null;
}

/** A lesson is finished once every quiz question has an answer (workspace.LessonStatus.Done). */
export const lessonDone = (l: LessonStatus) => l.answered >= l.questions;

/** What opens an objective's lesson: the lesson, else the page that has it written. */
export const lessonTarget = (objective: string, lesson: LessonStatus | undefined) => (lesson ? lessonKey(lesson.id) : todoKey(objective, "lesson"));

/**
 * What opens an objective's exercise, given the objective's tasks: the newest
 * unsolved one (an easier or harder one replaces the one before it), else the
 * last one, else the page that has one written.
 */
export const exerciseTarget = (objective: string, tasks: TaskInfo[]) =>
  (tasks.findLast((t) => !t.passed) ?? tasks.at(-1))?.id ?? todoKey(objective, "exercise");

// Calls that only read (or, like autosave, are safe to repeat) wait out a
// short outage, such as the server restarting, instead of failing: they retry
// for up to a minute while the server doesn't answer. Calls that change
// something fail at once, so nothing is done twice.
const SAFE_TO_REPEAT = new Set(["Activity", "Chat", "Environment", "Lesson", "ListWorkspaces", "Models", "Settings", "Task", "Workspace", "SaveWork"]);
const RETRY_MS = 1000;
const RETRY_FOR_MS = 60_000;

async function patiently<T>(call: () => Promise<T>): Promise<T> {
  const until = Date.now() + RETRY_FOR_MS;
  for (;;) {
    try {
      return await call();
    } catch (e) {
      if (!isOffline(e) || Date.now() > until) throw e;
      await new Promise((r) => setTimeout(r, RETRY_MS));
    }
  }
}

export const App: typeof Bindings = new Proxy(Bindings, {
  get(target, key, receiver) {
    const value = Reflect.get(target, key, receiver);
    if (typeof key !== "string" || !SAFE_TO_REPEAT.has(key) || typeof value !== "function") return value;
    return (...args: unknown[]) => patiently(() => value(...args));
  },
});

/**
 * The app's own server didn't answer (the request never got a reply), as
 * opposed to a call that failed. Callers retry quietly instead of showing it.
 */
export function isOffline(err: unknown): boolean {
  const text = err instanceof Error ? err.message : String(err);
  return /failed to fetch|networkerror|load failed|network request failed/i.test(text);
}

export function errorText(err: unknown): string {
  if (isOffline(err)) return "Can't reach Tutortop. Reload the page once it's running again.";
  const text = err instanceof Error ? err.message : typeof err === "string" ? err : JSON.stringify(err);
  // Go errors start lowercase; they're shown as sentences here.
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** Inserts or replaces a chat item by id (streaming text and tool updates reuse the id). */
export function upsertChat(items: ChatItem[], item: ChatItem): ChatItem[] {
  const i = items.findIndex((x) => x.id === item.id);
  if (i === -1) return [...items, item];
  const next = items.slice();
  next[i] = item;
  return next;
}

/**
 * The plan conversation after setup. The interview and the first plan, up to
 * the learner approving it, stay with the tutor but off screen. The match is
 * on the notice ApproveProposal posts (internal/app/app.go).
 */
export function afterSetup(items: ChatItem[]): ChatItem[] {
  const i = items.findIndex((x) => x.role === "notice" && x.text.startsWith("You approved the plan"));
  return i === -1 ? items : items.slice(i + 1);
}

/**
 * Merges the saved transcript with what we have live. Items still streaming
 * (assistant text, running tools) aren't saved yet, so we keep them at the end.
 */
export function mergeChat(saved: ChatItem[] | null, live: ChatItem[]): ChatItem[] {
  const out = saved ?? [];
  const ids = new Set(out.map((x) => x.id));
  return [...out, ...live.filter((x) => !ids.has(x.id))];
}

export function languageLabel(lang: string): string {
  return lang === "go" ? "Go" : lang === "python" ? "Python" : lang.length <= 2 ? lang.toUpperCase() : lang.charAt(0).toUpperCase() + lang.slice(1);
}

export function providerName(id: string): string {
  return id === "claude" ? "Claude Code" : id === "codex" ? "Codex" : id;
}

export function objectivesOf(c: Curriculum | null) {
  return (c?.modules ?? []).flatMap((m) => m.objectives ?? []);
}

/** Rough minutes per task size, as the coach is told to aim for. */
export const TASK_MINUTES: Record<string, number> = {
  small: 10,
  medium: 25,
  large: 60,
};

export const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
