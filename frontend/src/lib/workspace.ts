import type { Curriculum, LessonStatus } from "@/lib/api";

// The backend's names for a workspace's chat threads (internal/workspace).
// They are the wire format: ChatMessage and ThreadBusy events carry them, and
// ChatService.Chat, SendMessage and StopAgent take them.

/** The planning conversation: mission, curriculum and anything not written yet (workspace.PlanThread). */
export const PLAN_THREAD = "plan";

/** A lesson's conversation (workspace.LessonThread). A task's conversation is named by the task id. */
export const lessonThread = (lessonId: string) => `lesson-${lessonId}`;

/**
 * The background conversation that writes an objective's lesson and exercise
 * (workspace.WriterThread). It isn't shown as a chat; while it's busy, the
 * objective shows as being written.
 */
export const writerThread = (objective: string) => `write-${objective}`;
export const objectiveOfWriter = (thread: string) => (thread.startsWith("write-") ? thread.slice("write-".length) : null);

/** A lesson is finished once every quiz question has an answer (workspace.LessonStatus.Done). */
export const lessonDone = (l: LessonStatus) => l.answered >= l.questions;

export function objectivesOf(c: Curriculum | null) {
  return (c?.modules ?? []).flatMap((m) => m.objectives ?? []);
}
