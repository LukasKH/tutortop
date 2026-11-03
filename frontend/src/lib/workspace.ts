import type { Curriculum, LessonStatus, TaskInfo } from "@/lib/api";

// Keys and lookups for what's open in a workspace. They follow the backend's
// names for chat threads (internal/workspace).

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

export function objectivesOf(c: Curriculum | null) {
  return (c?.modules ?? []).flatMap((m) => m.objectives ?? []);
}
