import type { WorkspaceState } from "@/lib/api";
import { lessonDone, lessonThread } from "@/lib/workspace";

/**
 * What the centre pane has open: an exercise (task), a lesson, or an
 * objective's lesson or exercise that isn't written yet (todo), which shows a
 * page that can have it written now.
 */
export type OpenItem = { kind: "task"; id: string } | { kind: "lesson"; id: string } | { kind: "todo"; objective: string; part: Part };

export type Part = "lesson" | "exercise";

/** The open item's chat thread. Something not written yet has none; the planning conversation stands in. */
export function threadOf(item: OpenItem): string | null {
  if (item.kind === "task") return item.id;
  if (item.kind === "lesson") return lessonThread(item.id);
  return null;
}

/**
 * What the open item points at in the snapshot: its task or lesson, its
 * objective and module, the language to colour code in, and the next unsolved
 * task, preferring ones after the open task.
 */
export function describeItem(ws: WorkspaceState, item: OpenItem | null) {
  const tasks = ws.tasks ?? [];
  const task = item?.kind === "task" ? (tasks.find((t) => t.id === item.id) ?? null) : null;
  const lesson = item?.kind === "lesson" ? ((ws.lessons ?? []).find((l) => l.id === item.id) ?? null) : null;
  const objectiveId = item?.kind === "todo" ? item.objective : (lesson?.objective ?? task?.objective);
  const module = (ws.curriculum?.modules ?? []).find((m) => (m.objectives ?? []).some((o) => o.id === objectiveId)) ?? null;
  const objective = module?.objectives?.find((o) => o.id === objectiveId) ?? null;
  const after = task ? tasks.slice(tasks.indexOf(task) + 1) : tasks;
  return {
    task,
    lesson,
    module,
    objective,
    // The open exercise's language, else its objective's, else the plan's.
    lang: task?.language || objective?.language || ws.curriculum?.language || "",
    nextTask: after.find((t) => !t.passed) ?? tasks.find((t) => !t.passed && t !== task) ?? null,
  };
}

/** What opens an objective's lesson: the lesson, else the page that has it written. */
export function lessonOf(ws: WorkspaceState, objective: string): OpenItem {
  const lesson = (ws.lessons ?? []).find((l) => l.objective === objective);
  return lesson ? { kind: "lesson", id: lesson.id } : { kind: "todo", objective, part: "lesson" };
}

/**
 * What opens an objective's exercise: the newest unsolved task (an easier or
 * harder one replaces the one before it), else the last one, else the page
 * that has one written.
 */
export function exerciseOf(ws: WorkspaceState, objective: string): OpenItem {
  const tasks = (ws.tasks ?? []).filter((t) => t.objective === objective);
  const task = tasks.findLast((t) => !t.passed) ?? tasks.at(-1);
  return task ? { kind: "task", id: task.id } : { kind: "todo", objective, part: "exercise" };
}

/** A task, or first its objective's lesson if the learner hasn't finished it. */
export function viaLesson(ws: WorkspaceState, taskId: string): OpenItem {
  const objective = (ws.tasks ?? []).find((t) => t.id === taskId)?.objective;
  const lesson = (ws.lessons ?? []).find((l) => l.objective === objective && !lessonDone(l));
  return lesson ? { kind: "lesson", id: lesson.id } : { kind: "task", id: taskId };
}

/**
 * What clicking an objective in the curriculum opens, following the suggested
 * flow: its lesson until the quiz is done, then its exercise (the open one, if
 * it's one of the objective's); else whatever exists, else the lesson page
 * that has it written.
 */
export function entryOf(ws: WorkspaceState, objective: string, current: OpenItem | null): OpenItem {
  const lesson = (ws.lessons ?? []).find((l) => l.objective === objective);
  const passed = ws.progress.objectives?.[objective]?.status === "passed";
  if (lesson && !passed && !lessonDone(lesson)) return { kind: "lesson", id: lesson.id };
  const tasks = (ws.tasks ?? []).filter((t) => t.objective === objective);
  if (current?.kind === "task" && tasks.some((t) => t.id === current.id)) return current;
  return tasks.length ? exerciseOf(ws, objective) : lessonOf(ws, objective);
}

/**
 * Keeps the open item if it still exists. Something that wasn't written yet
 * becomes the real lesson or exercise once the tutor has written it, and stays
 * open while its objective is in the plan. Otherwise the default opens.
 */
export function resolveOpen(ws: WorkspaceState, item: OpenItem | null): OpenItem | null {
  const tasks = ws.tasks ?? [];
  const lessons = ws.lessons ?? [];
  switch (item?.kind) {
    case "task":
      return tasks.some((t) => t.id === item.id) ? item : defaultItem(ws);
    case "lesson":
      return lessons.some((l) => l.id === item.id) ? item : defaultItem(ws);
    case "todo": {
      const written = item.part === "lesson" ? lessons.find((l) => l.objective === item.objective) : tasks.find((t) => t.objective === item.objective);
      if (written) return { kind: item.part === "lesson" ? "lesson" : "task", id: written.id };
      const inPlan = (ws.curriculum?.modules ?? []).some((m) => (m.objectives ?? []).some((o) => o.id === item.objective));
      return inPlan ? item : defaultItem(ws);
    }
    default:
      return defaultItem(ws);
  }
}

/**
 * The item as one string: how it's remembered between sessions, and a React
 * key. A lesson's key is its chat thread; the format predates OpenItem, so
 * stored keys still read back.
 */
export function itemKey(item: OpenItem): string {
  if (item.kind === "todo") return `todo:${item.part}:${item.objective}`;
  return item.kind === "lesson" ? lessonThread(item.id) : item.id;
}

export function parseItemKey(key: string | null): OpenItem | null {
  if (!key) return null;
  const todo = key.match(/^todo:(lesson|exercise):(.+)$/);
  if (todo) return { kind: "todo", part: todo[1] as Part, objective: todo[2] };
  if (key.startsWith("lesson-")) return { kind: "lesson", id: key.slice("lesson-".length) };
  return { kind: "task", id: key };
}

export const sameItem = (a: OpenItem | null, b: OpenItem | null) => (a && b ? itemKey(a) === itemKey(b) : a === b);

/**
 * What to open by default: the first unsolved task, or its objective's lesson
 * if the learner hasn't finished that yet; else the first lesson without an
 * exercise; else the last task.
 */
function defaultItem(ws: WorkspaceState): OpenItem | null {
  const tasks = ws.tasks ?? [];
  const lessons = ws.lessons ?? [];
  const next = tasks.find((t) => !t.passed);
  if (next) return viaLesson(ws, next.id);
  const lesson = lessons.find((l) => !tasks.some((t) => t.objective === l.objective));
  if (lesson) return { kind: "lesson", id: lesson.id };
  const last = tasks.at(-1);
  return last ? { kind: "task", id: last.id } : null;
}
