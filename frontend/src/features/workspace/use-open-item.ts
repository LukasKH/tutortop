import { useState } from "react";
import { AppEvents, useEvent, type LessonStatus, type Objective, type Questions, type WorkspaceState } from "@/lib/api";
import { PLAN_THREAD } from "@/lib/workspace";
import { readStored, useStored } from "@/hooks/use-stored";
import { describeItem, itemKey, parseItemKey, resolveOpen, sameItem, threadOf, type OpenItem, type Part } from "@/features/workspace/open-item";
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
  // Each snapshot checks the open item still exists, and what it picks
  // instead is remembered, so a default doesn't move on its own. Until the
  // first snapshot, the stored item is unchecked. Older versions stored it
  // as "task.<id>", though it can be any item; that's read once, so the
  // learner's open item survives the update.
  const [storedKey, setStoredKey] = useStored<string | null>(`open-item.${id}`, () => readStored(`task.${id}`, null));
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

/** What the centre pane shows. */
export type Center =
  | { kind: "setup" }
  | { kind: "proposal"; proposal: NonNullable<WorkspaceState["proposal"]> }
  | { kind: "plan" }
  | { kind: "questions"; questions: Questions }
  | { kind: "not-written"; objective: Objective; part: Part }
  | { kind: "lesson"; lesson: LessonStatus }
  | { kind: "task"; id: string }
  | { kind: "empty" };

/**
 * Decides the centre pane. Until the first plan is approved (setup),
 * everything happens there: the questions, the tutor's work, then the plan to
 * review. After that, the plan's pages when chosen, else the tutor's
 * questions if nothing is open, else the open item.
 */
export function centerOf(ws: WorkspaceState, view: CenterView, item: OpenItem | null, questions: Questions | null): Center {
  if (!ws.curriculum) {
    if (view === "plan") return { kind: "plan" };
    return ws.proposal ? { kind: "proposal", proposal: ws.proposal } : { kind: "setup" };
  }
  if (view === "proposal" && ws.proposal) return { kind: "proposal", proposal: ws.proposal };
  if (view === "plan") return { kind: "plan" };
  if (questions && questionsInCenter(view, item, questions)) return { kind: "questions", questions };
  const { lesson, objective } = describeItem(ws, item);
  if (item?.kind === "todo" && objective) return { kind: "not-written", objective, part: item.part };
  if (lesson) return { kind: "lesson", lesson };
  if (item?.kind === "task") return { kind: "task", id: item.id };
  return { kind: "empty" };
}

/**
 * Before the first task, the tutor's questions are the main thing to do, so
 * they get the centre pane instead of the narrow chat column. Otherwise they
 * appear in the chat of the thread that asked.
 */
export const questionsInCenter = (view: CenterView, item: OpenItem | null, questions: Questions | null) => !!questions && !item && view === "item";
