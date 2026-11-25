import { useEffect, useEffectEvent } from "react";
import { Events } from "@wailsio/runtime";
// The generated bindings, one module per Go package. Services and types are
// re-exported below under their Go names.
import * as app from "@bindings/github.com/LukasKH/tutortop/internal/app";
import * as threads from "@bindings/github.com/LukasKH/tutortop/internal/agent/threads";
import * as settings from "@bindings/github.com/LukasKH/tutortop/internal/settings";
import * as copilot from "@bindings/github.com/LukasKH/tutortop/internal/copilot";
import * as update from "@bindings/github.com/LukasKH/tutortop/internal/update";
import type { WorkspaceSnapshot } from "@bindings/github.com/LukasKH/tutortop/internal/app";
import type { AgentSetup, Environment, Prefs, ProviderStatus } from "@bindings/github.com/LukasKH/tutortop/internal/settings";
import type { Completion, DeviceCode, Plan, Setup, Suggestion } from "@bindings/github.com/LukasKH/tutortop/internal/copilot";
import type { State } from "@bindings/github.com/LukasKH/tutortop/internal/update";
import type { SandboxCheck, Working } from "@bindings/github.com/LukasKH/tutortop/internal/events";
import type { Check, Limit, Model } from "@bindings/github.com/LukasKH/tutortop/internal/agent";
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
import type { Result, TestResult } from "@bindings/github.com/LukasKH/tutortop/internal/sandbox";

// The backend's enums, compared against instead of their string values.
export { ChatMark, ChatRole, ChatStatus, ObjectiveStatus, TaskShape, TaskSize } from "@bindings/github.com/LukasKH/tutortop/internal/workspace";
export { CheckKind } from "@bindings/github.com/LukasKH/tutortop/internal/events";
export { Status } from "@bindings/github.com/LukasKH/tutortop/internal/sandbox";
export { Phase } from "@bindings/github.com/LukasKH/tutortop/internal/update";

export type {
  Activity,
  AgentSetup,
  ChatItem,
  Check,
  Completion,
  Curriculum,
  DeviceCode,
  Environment,
  LessonDetail,
  LessonStatus,
  Limit,
  Model,
  Objective,
  Plan,
  Progress,
  ProviderStatus,
  Question,
  Questions,
  QuizResult,
  Result,
  Prefs,
  SandboxCheck,
  Setup,
  Source,
  State,
  Suggestion,
  Summary,
  TaskDetail,
  TaskStatus,
  TestResult,
  Working,
  WorkspaceSnapshot,
};

// The backend's services. Calls that only read, or are safe to repeat like
// autosave, wait out a short outage such as a server restart (patiently).
// Calls that change something fail at once, so nothing is done twice.

export const WorkspaceService = {
  ...app.WorkspaceService,
  ListWorkspaces: patiently(app.WorkspaceService.ListWorkspaces),
  Activity: patiently(app.WorkspaceService.Activity),
  Workspace: patiently(app.WorkspaceService.Workspace),
};

export const TaskService = {
  ...app.TaskService,
  Task: patiently(app.TaskService.Task),
  SaveWork: patiently(app.TaskService.SaveWork),
};

export const LessonService = { ...app.LessonService, Lesson: patiently(app.LessonService.Lesson) };

export const ChatService = { ...threads.ChatService, Chat: patiently(threads.ChatService.Chat) };

// settings.Service, editor.Service and copilot.Service are all named Service
// in Go, so they take their package's name here.
export const SettingsService = {
  ...settings.Service,
  Load: patiently(settings.Service.Load),
  Models: patiently(settings.Service.Models),
  Environment: patiently(settings.Service.Environment),
  Check: patiently(settings.Service.Check),
};

export { Service as EditorService } from "@bindings/github.com/LukasKH/tutortop/internal/editor";

export const CopilotService = { ...copilot.Service, Setup: patiently(copilot.Service.Setup) };

export const UpdateService = { ...update.Service, Status: patiently(update.Service.Status) };

export function errorText(err: unknown): string {
  if (isOffline(err)) return "Can't reach Tutortop. Reload the page once it's running again.";
  const text = err instanceof Error ? err.message : typeof err === "string" ? err : JSON.stringify(err);
  // Go errors start lowercase; they're shown as sentences here.
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * The names of the backend's events. Importing them also registers their data types with the runtime.
 * Named AppEvents, not events as in Go, to keep it apart from the runtime's Events that listens to them.
 */
export * as AppEvents from "@bindings/events";

/** Listens to one of the backend's events (internal/events) for the component's lifetime. */
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
