import { useCallback, useEffect, useReducer } from "react";
import { App, AppEvents, errorText, useEvent } from "@/lib/api";
import { initialWorkspace, reduceWorkspace, tutorBusy, writingObjectives } from "@/features/workspace/workspace-state";

/**
 * A workspace as the backend reports it (LiveWorkspace), kept current by its
 * events, and the calls that change it. Events that change the snapshot
 * refetch it; the rest update the state directly (workspace-state.ts).
 */
export function useWorkspace(id: string) {
  const [state, dispatch] = useReducer(reduceWorkspace, initialWorkspace);
  const fail = useCallback((e: unknown) => dispatch({ type: "failed", error: errorText(e) }), []);

  /** Refetches the snapshot; resolves to it, or null if the call failed. */
  const refresh = useCallback(async () => {
    try {
      const ws = await App.Workspace(id);
      dispatch({ type: "snapshot", ws });
      return ws;
    } catch (e) {
      fail(e);
      return null;
    }
  }, [id, fail]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Published tasks and lessons also open or get pointed out (use-open-item.ts).
  useEvent(AppEvents.WorkspaceChanged, ({ workspace }) => workspace === id && refresh());
  useEvent(AppEvents.TaskPublished, ({ workspace }) => workspace === id && refresh());
  useEvent(AppEvents.LessonPublished, ({ workspace }) => workspace === id && refresh());

  useEvent(AppEvents.ThreadBusy, ({ workspace, thread, busy, seq }) => {
    if (workspace === id) dispatch({ type: "busy", thread, busy, seq });
  });
  useEvent(AppEvents.CheckStatus, ({ workspace, check, seq }) => {
    if (workspace === id) dispatch({ type: "check", check, seq });
  });
  useEvent(AppEvents.ChatMessage, ({ workspace, item }) => {
    if (workspace === id) dispatch({ type: "chat", item });
  });
  useEvent(AppEvents.QuestionsAsked, ({ workspace, questions }) => {
    if (workspace === id) dispatch({ type: "questions", questions });
  });

  async function answerQuestions(answers: Record<string, string>) {
    await App.AnswerQuestions(id, answers);
    dispatch({ type: "questions", questions: null });
  }

  function dismissQuestions() {
    App.DismissQuestions(id).catch(fail);
    dispatch({ type: "questions", questions: null });
  }

  return {
    ...state,
    /** Objectives being written in the background. */
    writing: writingObjectives(state.busy),
    /** A tutor (not a writer) is working somewhere in the workspace. */
    tutorBusy: tutorBusy(state.busy),
    fail,
    refresh,
    answerQuestions,
    dismissQuestions,
    /** Clears a finished toolchain check from the status line, e.g. once the learner moves on. */
    dismissFinishedCheck: () => dispatch({ type: "dismissFinishedCheck" }),
  };
}

/** What useWorkspace returns: the LiveWorkspace, kept current, and the calls that change it. Not workspace.Workspace or WorkspaceSnapshot. */
export type UseWorkspaceResult = ReturnType<typeof useWorkspace>;
