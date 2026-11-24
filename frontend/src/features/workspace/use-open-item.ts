import { useState } from "react";
import {
  AppEvents,
  useEvent,
  type Curriculum,
  type LessonStatus,
  type Objective,
  type Progress,
  type Questions,
  type Source,
  type TaskStatus,
  type WorkspaceSnapshot,
} from "@/lib/api";
import { PLAN_THREAD } from "@/lib/workspace";
import { useStored } from "@/hooks/use-stored";
import {
  exerciseOf,
  itemKey,
  lessonOf,
  parseItemKey,
  resolveOpen,
  sameItem,
  threadOf,
  viaLesson,
  type ItemDetails,
  type OpenItem,
  type Part,
} from "@/features/workspace/open-item";
import type { UseWorkspaceResult } from "@/features/workspace/use-workspace";

/** The centre pane shows the open item, or one of the plan's pages. */
export type CenterView = "item" | "plan" | "proposal";

/** Something new the tutor wrote while the learner was busy elsewhere, pointed out in the chat's status line. */
export type NewItem = { item: OpenItem; title: string };

/**
 * What the learner has open in a workspace: the item (remembered per
 * workspace), the centre's view, and the chat thread that goes with them.
 * New lessons and exercises open, or are pointed out, as they're published.
 */
export function useOpenItem(id: string, { ws, refresh, dismissFinishedCheck }: Pick<UseWorkspaceResult, "ws" | "refresh" | "dismissFinishedCheck">) {
  // A replacement for a missing item is stored, so a default doesn't move on
  // its own.
  const [storedKey, setStoredKey] = useStored<string | null>(`open-item.${id}`, null);
  const stored = parseItemKey(storedKey);
  const item = ws ? resolveOpen(ws, stored) : stored;
  if (!sameItem(item, stored)) setStoredKey(item && itemKey(item));

  // Until the learner picks a view, a proposal waiting for review comes first.
  const [chosenView, setView] = useState<CenterView | null>(null);
  const view = chosenView ?? (ws?.proposal ? "proposal" : "item");
  const [newItem, setNewItem] = useState<NewItem | null>(null);

  // The chat follows the centre pane: an open exercise or lesson has its own
  // conversation; the plan's pages and anything not written yet use the
  // planning conversation.
  const thread = (ws && view === "item" && item && threadOf(item)) || PLAN_THREAD;

  function open(next: OpenItem) {
    setStoredKey(itemKey(next));
    setView("item");
    setNewItem((n) => (n && sameItem(n.item, next) ? null : n));
    dismissFinishedCheck();
  }

  // New work opens at once when it's what the learner is waiting for: its
  // page was open while it was being written, or nothing is open. Otherwise
  // it's pointed out, but only once the open task is solved: lessons and
  // tasks are written ahead in the background. An announced lesson comes first.
  const waitingFor = (part: Part, objective: string) => !item || (item.kind === "todo" && item.part === part && item.objective === objective);
  const openTaskSolved = item?.kind === "task" && !!ws?.tasks?.find((t) => t.id === item.id)?.passed;

  useEvent(AppEvents.TaskPublished, ({ workspace, task }) => {
    if (workspace !== id) return;
    const published: OpenItem = { kind: "task", id: task.id };
    if (waitingFor("exercise", task.objective)) open(published);
    else if (openTaskSolved) setNewItem((cur) => (cur?.item.kind === "lesson" ? cur : { item: published, title: task.title }));
  });

  useEvent(AppEvents.LessonPublished, ({ workspace, lesson }) => {
    if (workspace !== id) return;
    const published: OpenItem = { kind: "lesson", id: lesson.id };
    if (waitingFor("lesson", lesson.objective)) open(published);
    else if (openTaskSolved) setNewItem({ item: published, title: lesson.title });
  });

  useEvent(AppEvents.CurriculumProposed, ({ workspace }) => {
    if (workspace === id) refresh().then((s) => s?.proposal && setView("proposal"));
  });

  return { item, view, thread, newItem, open, setView };
}

export type UseOpenItemResult = ReturnType<typeof useOpenItem>;

/**
 * What the centre pane shows, and everything it shows from the snapshot.
 * What changes between snapshots (busy threads, checks) and the calls come
 * from useWorkspace.
 */
export type Center =
  | { kind: "setup"; questions: Questions | null }
  | { kind: "proposal"; proposal: Curriculum; current: Curriculum | null; progress: Progress; mission: string; resources: string }
  | {
      kind: "plan";
      ws: WorkspaceSnapshot;
      /** "Back" needs an open item to go back to, once there's a plan. */
      canGoBack: boolean;
      /** Before the first plan there's no chat panel to ask. */
      canAskTutor: boolean;
    }
  | { kind: "questions"; questions: Questions }
  | {
      kind: "not-written";
      objective: Objective;
      part: Part;
      moduleTitle: string;
      /** The objective's other part, once it's written: the exercise from the lesson's page, or the lesson. */
      other: OpenItem | null;
    }
  | {
      kind: "lesson";
      lesson: LessonStatus;
      moduleTitle: string;
      icon: string;
      lang: string;
      references: Source[] | null;
      /** What "Exercise" opens: the objective's exercise, or the page that has it written. */
      exercise: OpenItem;
    }
  | {
      kind: "task";
      id: string;
      passed: boolean;
      moduleTitle: string;
      nextTask: TaskStatus | null;
      /** What "Next task" opens: the next task, or first its objective's lesson if that isn't finished. */
      openNext: OpenItem | null;
      moreComing: boolean;
    }
  | { kind: "empty"; mission: string };

/**
 * Decides the centre pane. Until the first plan is approved (setup),
 * everything happens there: the questions, the tutor's work, then the plan to
 * review. After that, the plan's pages when chosen, else the tutor's
 * questions if placeQuestions put them here, else the open item.
 */
export function centerOf(ws: WorkspaceSnapshot, view: CenterView, open: ItemDetails, centred: Questions | null): Center {
  const review = (proposal: Curriculum): Center => ({
    kind: "proposal",
    proposal,
    current: ws.curriculum,
    progress: ws.progress,
    mission: ws.mission,
    resources: ws.resources,
  });
  if (!ws.curriculum) {
    if (view === "plan") return { kind: "plan", ws, canGoBack: true, canAskTutor: false };
    return ws.proposal ? review(ws.proposal) : { kind: "setup", questions: centred };
  }
  if (view === "proposal" && ws.proposal) return review(ws.proposal);
  if (view === "plan") return { kind: "plan", ws, canGoBack: !!open.item, canAskTutor: true };
  if (centred) return { kind: "questions", questions: centred };

  const { item, lesson, objective, module } = open;
  const moduleTitle = module?.title ?? "";
  if (item?.kind === "todo" && objective) {
    const other = item.part === "lesson" ? exerciseOf(ws, objective.id) : lessonOf(ws, objective.id);
    return { kind: "not-written", objective, part: item.part, moduleTitle, other: other.kind === "todo" ? null : other };
  }
  if (lesson) {
    const exercise = exerciseOf(ws, lesson.objective);
    return { kind: "lesson", lesson, moduleTitle, icon: ws.icon, lang: open.lang, references: module?.references ?? null, exercise };
  }
  if (item?.kind === "task") {
    const { task, nextTask } = open;
    const openNext = nextTask && viaLesson(ws, nextTask.id);
    return { kind: "task", id: item.id, passed: !!task?.passed, moduleTitle, nextTask, openNext, moreComing: ws.progress.passed < ws.progress.total };
  }
  return { kind: "empty", mission: ws.mission };
}

/** Where the tutor's open questions show on the workspace screen. */
export type QuestionsPlace = {
  /** The centre pane shows the form: the setup page, or the questions page while nothing is open. */
  center: boolean;
  /** The thread whose chat shows the form: the one that asked. Null when they're in the centre, or there are none. */
  chat: string | null;
  /** How many questions "Your plan" counts in the curriculum rail: the plan asked while another chat is shown. */
  planBadge: number;
};

/**
 * Decides where the tutor's questions show. Before the first task they're
 * the main thing to do, so they get the centre pane instead of the narrow
 * chat column. Otherwise they wait in the chat of the thread that asked,
 * shown or not, and the plan's are counted on "Your plan".
 */
export function placeQuestions(ws: WorkspaceSnapshot, view: CenterView, item: OpenItem | null, thread: string, questions: Questions | null): QuestionsPlace {
  const nowhere: QuestionsPlace = { center: false, chat: null, planBadge: 0 };
  if (!questions) return nowhere;
  // During setup there's no chat beside the centre. The setup page shows them, unless the plan or a proposal is open instead (centerOf).
  if (!ws.curriculum) return { ...nowhere, center: view !== "plan" && !ws.proposal };
  if (view === "item" && !item) return { ...nowhere, center: true };
  const planBadge = questions.thread === PLAN_THREAD && thread !== PLAN_THREAD ? (questions.questions ?? []).length : 0;
  return { center: false, chat: questions.thread, planBadge };
}
