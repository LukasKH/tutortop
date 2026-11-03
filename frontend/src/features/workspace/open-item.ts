import type { WorkspaceState } from "@/lib/api";
import { exerciseTarget, lessonDone, lessonKey, lessonOfKey, lessonTarget, todoOfKey } from "@/lib/workspace";

/**
 * What the open key points at: a task, a lesson, or a part not written yet
 * (todo). Also its objective and module, the language to colour code in, and
 * the next unsolved task, preferring ones after the open task.
 */
export function openItem(ws: WorkspaceState, key: string | null) {
  const tasks = ws.tasks ?? [];
  const todo = todoOfKey(key);
  const lessonId = lessonOfKey(key);
  const lesson = (ws.lessons ?? []).find((l) => l.id === lessonId) ?? null;
  const task = tasks.find((t) => t.id === key) ?? null;
  const objectiveId = todo?.objective ?? lesson?.objective ?? task?.objective;
  const module = (ws.curriculum?.modules ?? []).find((m) => (m.objectives ?? []).some((o) => o.id === objectiveId)) ?? null;
  const objective = module?.objectives?.find((o) => o.id === objectiveId) ?? null;
  const index = tasks.findIndex((t) => t.id === key);
  return {
    todo,
    lesson,
    task,
    module,
    objective,
    // The open exercise's language, else its objective's, else the plan's.
    lang: task?.language || objective?.language || ws.curriculum?.language || "",
    nextTask: tasks.slice(index + 1).find((t) => !t.passed) ?? tasks.find((t) => !t.passed && t.id !== key) ?? null,
  };
}

/** What opens an objective's lesson: the lesson, else the page that has it written. */
export const lessonOf = (ws: WorkspaceState, objective: string) =>
  lessonTarget(
    objective,
    (ws.lessons ?? []).find((l) => l.objective === objective),
  );

/** What opens an objective's exercise: its newest unsolved task, else the page that has one written. */
export const exerciseOf = (ws: WorkspaceState, objective: string) =>
  exerciseTarget(
    objective,
    (ws.tasks ?? []).filter((t) => t.objective === objective),
  );

/** A task, or first its objective's lesson if the learner hasn't finished it. */
export function viaLesson(ws: WorkspaceState, taskId: string): string {
  const objective = (ws.tasks ?? []).find((t) => t.id === taskId)?.objective;
  const lesson = (ws.lessons ?? []).find((l) => l.objective === objective && !lessonDone(l));
  return lesson ? lessonKey(lesson.id) : taskId;
}

/**
 * Keeps the open item if it still exists. Something that wasn't written yet
 * becomes the real lesson or exercise once the tutor has written it, and stays
 * open while its objective is in the plan.
 */
export function resolveOpen(s: WorkspaceState, cur: string | null): string | null {
  const tasks = s.tasks ?? [];
  const lessons = s.lessons ?? [];
  const todo = todoOfKey(cur);
  if (todo) {
    const lesson = lessons.find((l) => l.objective === todo.objective);
    const written = todo.part === "lesson" ? lesson && lessonKey(lesson.id) : tasks.find((t) => t.objective === todo.objective)?.id;
    if (written) return written;
    const inPlan = (s.curriculum?.modules ?? []).some((m) => (m.objectives ?? []).some((o) => o.id === todo.objective));
    return inPlan ? cur : defaultItem(s);
  }
  const exists = (key: string) => tasks.some((t) => t.id === key) || lessons.some((l) => lessonKey(l.id) === key);
  return cur && exists(cur) ? cur : defaultItem(s);
}

/**
 * What to open by default: the first unsolved task, or its objective's lesson
 * if the learner hasn't finished that yet; else the last task or lesson.
 */
function defaultItem(s: WorkspaceState): string | null {
  const tasks = s.tasks ?? [];
  const lessons = s.lessons ?? [];
  const unfinished = (o: string) => lessons.find((l) => l.objective === o && !lessonDone(l));
  const next = tasks.find((t) => !t.passed);
  if (next) {
    const l = unfinished(next.objective);
    return l ? lessonKey(l.id) : next.id;
  }
  const lesson = lessons.find((l) => !tasks.some((t) => t.objective === l.objective)) ?? null;
  if (lesson) return lessonKey(lesson.id);
  return tasks.at(-1)?.id ?? null;
}
