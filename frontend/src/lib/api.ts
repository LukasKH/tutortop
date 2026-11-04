import { useEffect, useEffectEvent } from "react";
import { Events } from "@wailsio/runtime";
import * as Bindings from "@bindings/github.com/LukasKH/tutortop/internal/app";
import type { Environment, ProviderStatus, SandboxCheck, Settings, WorkspaceSnapshot } from "@bindings/github.com/LukasKH/tutortop/internal/app";
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

/** Who said a chat item: the learner, the tutor, a tool call worth showing, or the app; how a tool call went; and what an item is, for code that looks for it. */
export { ChatMark, ChatRole, ChatStatus } from "@bindings/github.com/LukasKH/tutortop/internal/workspace";
/** What a sandbox check is for: a writer's new task or a toolchain. */
export { CheckKind } from "@bindings/github.com/LukasKH/tutortop/internal/app";

export type {
  Activity,
  ChatItem,
  Curriculum,
  Environment,
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
  SandboxCheck,
  Source,
  Settings,
  Summary,
  TaskDetail,
  TaskStatus,
  TestResult,
  WorkspaceSnapshot,
};

// The backend's services (the generated bindings), one per area. Calls that
// only read (or, like autosave, are safe to repeat) wait out a short outage,
// such as the server restarting, instead of failing (patiently). Calls that
// change something fail at once, so nothing is done twice.

export const WorkspaceService = {
  ...Bindings.WorkspaceService,
  ListWorkspaces: patiently(Bindings.WorkspaceService.ListWorkspaces),
  Activity: patiently(Bindings.WorkspaceService.Activity),
  Workspace: patiently(Bindings.WorkspaceService.Workspace),
};

export const TaskService = {
  ...Bindings.TaskService,
  Task: patiently(Bindings.TaskService.Task),
  SaveWork: patiently(Bindings.TaskService.SaveWork),
};

export const LessonService = { ...Bindings.LessonService, Lesson: patiently(Bindings.LessonService.Lesson) };

export const ChatService = { ...Bindings.ChatService, Chat: patiently(Bindings.ChatService.Chat) };

export const SettingsService = {
  ...Bindings.SettingsService,
  Settings: patiently(Bindings.SettingsService.Settings),
  Models: patiently(Bindings.SettingsService.Models),
  Environment: patiently(Bindings.SettingsService.Environment),
};

export { EditorService } from "@bindings/github.com/LukasKH/tutortop/internal/app";

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

const RETRY_MS = 1000;
const RETRY_FOR_MS = 60_000;

/** Wraps a call to retry for up to a minute while the server doesn't answer. */
function patiently<A extends unknown[], R>(call: (...args: A) => Promise<R>): (...args: A) => Promise<R> {
  return async (...args) => {
    const until = Date.now() + RETRY_FOR_MS;
    for (;;) {
      try {
        return await call(...args);
      } catch (e) {
        if (!isOffline(e) || Date.now() > until) throw e;
        await new Promise((r) => setTimeout(r, RETRY_MS));
      }
    }
  };
}

/**
 * The app's own server didn't answer (the request never got a reply), as
 * opposed to a call that failed. Callers retry quietly instead of showing it.
 */
function isOffline(err: unknown): boolean {
  const text = err instanceof Error ? err.message : String(err);
  return /failed to fetch|networkerror|load failed|network request failed/i.test(text);
}
