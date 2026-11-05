import { createContext, use, useCallback, useEffect, useReducer } from "react";
import { ChatService, WorkspaceService, AppEvents, errorText, useEvent } from "@/lib/api";
import { initialWorkspace, isBusy, reduceWorkspace, tutorBusy, writingObjectives } from "@/features/workspace/workspace-state";

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
      const ws = await WorkspaceService.Workspace(id);
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
  // chat:message carries every thread's messages, and two hooks split them by
  // thread kind: use-chat-threads.ts keeps the chats' transcripts and drops
  // writers'. A writer has no chat; all the screen shows of its messages is
  // why it failed, kept here by objective because a writer starting again
  // clears it (reduceWorkspace).
  useEvent(AppEvents.ChatMessage, ({ workspace, item }) => {
    if (workspace === id) dispatch({ type: "chat", item });
  });
  useEvent(AppEvents.QuestionsAsked, ({ workspace, questions }) => {
    if (workspace === id) dispatch({ type: "questions", questions });
  });

  async function answerQuestions(answers: Record<string, string>) {
    await ChatService.AnswerQuestions(id, answers);
    dispatch({ type: "questions", questions: null });
  }

  function dismissQuestions() {
    ChatService.DismissQuestions(id).catch(fail);
    dispatch({ type: "questions", questions: null });
  }

  return {
    ...state,
    // Who is working: read busy only through these (workspace-state.ts).
    isBusy: (thread: string) => isBusy(state, thread),
    tutorBusy: tutorBusy(state),
    writing: writingObjectives(state),
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

/** The open workspace's id and useWorkspace's result. WorkspaceView provides it to the panes inside it. */
export const LiveWorkspaceContext = createContext<{ id: string; live: UseWorkspaceResult } | null>(null);

export function useLiveWorkspace() {
  const ws = use(LiveWorkspaceContext);
  if (!ws) throw new Error("useLiveWorkspace is only for components inside WorkspaceView");
  return ws;
}
