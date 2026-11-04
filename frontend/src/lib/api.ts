import { useEffect, useEffectEvent } from "react";
import { Events } from "@wailsio/runtime";
import { App as Bindings } from "@bindings/github.com/LukasKH/tutortop/internal/app";
import type { Environment, Gate, ProviderStatus, Settings, WorkspaceState } from "@bindings/github.com/LukasKH/tutortop/internal/app";
import type { Model } from "@bindings/github.com/LukasKH/tutortop/internal/agent";
import type {
  Activity,
  ChatItem,
  Curriculum,
  LessonDetail,
  LessonStatus,
  Objective,
  Progress,
  Question,
  Questions,
  QuizResult,
  Source,
  Summary,
  TaskDetail,
  TaskStatus,
} from "@bindings/github.com/LukasKH/tutortop/internal/workspace";
import type { Result, TestResult } from "@bindings/github.com/LukasKH/tutortop/internal/runner";

/** Who said a chat item: the learner, the tutor, a tool call worth showing, or the app. */
export { ChatRole } from "@bindings/github.com/LukasKH/tutortop/internal/workspace";

export type {
  Activity,
  ChatItem,
  Curriculum,
  Environment,
  Gate,
  LessonDetail,
  LessonStatus,
  Model,
  Objective,
  Progress,
  ProviderStatus,
  Question,
  Questions,
  QuizResult,
  Result,
  Source,
  Settings,
  Summary,
  TaskDetail,
  TaskStatus,
  TestResult,
  WorkspaceState,
};

/**
 * The backend's methods (the generated bindings). Calls that only read retry
 * quietly while the app's server is briefly unreachable.
 */
export const App: typeof Bindings = new Proxy(Bindings, {
  get(target, key, receiver) {
    const value = Reflect.get(target, key, receiver);
    if (typeof key !== "string" || !SAFE_TO_REPEAT.has(key) || typeof value !== "function") return value;
    return (...args: unknown[]) => patiently(() => value(...args));
  },
});

export function errorText(err: unknown): string {
  if (isOffline(err)) return "Can't reach Tutortop. Reload the page once it's running again.";
  const text = err instanceof Error ? err.message : typeof err === "string" ? err : JSON.stringify(err);
  // Go errors start lowercase; they're shown as sentences here.
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** The names of the backend's events. Importing them also registers their data types with the runtime. */
export * as AppEvents from "@bindings/events";

/** Listens to one of the backend's events (internal/app/events.go) for the component's lifetime. */
export function useEvent<E extends keyof Events.CustomEvents>(name: E, handler: (data: Events.CustomEvents[E]) => void) {
  const onEvent = useEffectEvent(handler);
  useEffect(() => Events.On(name, (ev) => onEvent(ev.data)), [name]);
}

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

/**
 * The app's own server didn't answer (the request never got a reply), as
 * opposed to a call that failed. Callers retry quietly instead of showing it.
 */
function isOffline(err: unknown): boolean {
  const text = err instanceof Error ? err.message : String(err);
  return /failed to fetch|networkerror|load failed|network request failed/i.test(text);
}
