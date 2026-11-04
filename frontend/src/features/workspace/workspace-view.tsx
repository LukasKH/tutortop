import { useRef, useState, type CSSProperties } from "react";
import { cn } from "cn";
import { afterSetup } from "@/lib/chat";
import { App, AppEvents, useEvent, type WorkspaceState } from "@/lib/api";
import { lessonKey, objectiveOfWriter, PLAN_THREAD, todoOfKey, writerThread, type NewItem } from "@/lib/workspace";
import { exerciseOf, lessonOf, openItem, resolveOpen, viaLesson } from "@/features/workspace/open-item";
import { useChatPanel } from "@/features/workspace/use-chat-panel";
import { useChatThreads } from "@/features/workspace/use-chat-threads";
import { useWorkspace } from "@/features/workspace/use-workspace";
import { Sidebar } from "@/features/workspace/sidebar";
import { TaskPanel } from "@/features/task/task-panel";
import { LessonPanel } from "@/features/lesson/lesson-panel";
import { NotWritten } from "@/features/workspace/not-written";
import { PlanView } from "@/features/workspace/plan-view";
import { ProposalView } from "@/features/workspace/proposal-view";
import { ChatPanel, ChatRail, StatusLine, type Attention } from "@/features/tutor/chat-panel";
import { Markdown } from "@/components/markdown";
import { QuestionsForm } from "@/features/workspace/questions";
import { Setup } from "@/features/workspace/setup";
import { Placeholder } from "@/components/placeholder";
import { PageIntro, ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { Splitter } from "@/components/splitter";
import { useStored } from "@/hooks/use-stored";

export type CenterView = "task" | "plan" | "proposal";

type Props = {
  id: string;
  /** The tutor as configured in Settings, e.g. "Claude Code · Opus · High". */
  tutor: string;
  onSettings: () => void;
  onBack: () => void;
};

/** A workspace: the curriculum rail, the open lesson, exercise or plan page, and the tutor beside it. */
export function WorkspaceView({ id, tutor, onSettings, onBack }: Props) {
  const { ws, error, fail, refresh, busy, checks, writeFailed, gate, dismissFinishedGate, questions, answerQuestions, dismissQuestions } = useWorkspace(id);
  // The open task or lesson (by key), remembered per workspace. Each snapshot
  // checks it still exists; until the first one, the stored key is unchecked.
  const [storedId, setTaskId] = useStored<string | null>(`task.${id}`, null);
  const taskId = ws ? resolveOpen(ws, storedId) : storedId;
  // Until the learner picks a view, a proposal waiting for review comes first.
  const [chosenView, setView] = useState<CenterView | null>(null);
  const view = chosenView ?? (ws?.proposal ? "proposal" : "task");
  const [newTask, setNewTask] = useState<NewItem | null>(null);
  const [draft, setDraft] = useState("");
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const chat = useChatPanel();

  // The chat follows the centre pane: an open exercise or lesson has its own
  // conversation; the plan page, plan review, and anything not written yet
  // use the planning conversation. Writing happens apart from the chat. Until the
  // workspace loads, the stored key is unchecked.
  const todo = todoOfKey(taskId);
  const planContext = view !== "task" || !taskId || !!todo;
  const thread = ws && !planContext ? taskId! : PLAN_THREAD;
  const { threads, unread } = useChatThreads(id, thread, { ready: ws !== null, visible: chat.open, onError: fail });

  useEvent(AppEvents.CurriculumProposed, ({ workspace }) => {
    if (workspace === id) refresh().then((s) => s?.proposal && setView("proposal"));
  });

  // New work opens at once when it's what the learner is waiting for. Otherwise
  // it's pointed out, but only once the open task is solved: lessons and tasks
  // are written ahead in the background. An announced lesson comes first.
  useEvent(AppEvents.TaskPublished, ({ workspace, task }) => {
    if (workspace !== id) return;
    refresh();
    if ((todo?.part === "exercise" && todo.objective === task.objective) || !taskId) openTask(task.id);
    else if (openTaskSolved()) setNewTask((cur) => (cur?.kind === "lesson" ? cur : { id: task.id, title: task.title, kind: "task" }));
  });

  useEvent(AppEvents.LessonPublished, ({ workspace, lesson }) => {
    if (workspace !== id) return;
    refresh();
    const key = lessonKey(lesson.id);
    if ((todo?.part === "lesson" && todo.objective === lesson.objective) || !taskId) openTask(key);
    else if (openTaskSolved()) setNewTask({ id: key, title: lesson.title, kind: "lesson" });
  });

  const openTaskSolved = () => !!ws?.tasks?.find((t) => t.id === taskId)?.passed;

  function openTask(key: string) {
    setTaskId(key);
    setView("task");
    setNewTask((t) => (t?.id === key ? null : t));
    dismissFinishedGate();
  }

  if (!ws) {
    return (
      <Placeholder error={error} loading="Opening workspace…">
        <Button variant="outline" onClick={onBack}>
          Back to workspaces
        </Button>
      </Placeholder>
    );
  }

  const open = openItem(ws, taskId);
  const { lesson: openLesson, task: openTaskInfo, module: openModule, objective, lang } = open;
  const moduleTitle = openModule?.title ?? "";

  // Before the first task, the tutor's questions are the main thing to do, so
  // they get the centre pane instead of the narrow chat column. Otherwise they
  // appear in the chat of the thread that asked.
  const questionsThread = questions?.thread || PLAN_THREAD;
  const questionsInCenter = !!questions && !taskId && view === "task";
  const questionsPending = !!questions && !questionsInCenter;
  const planAsked = questionsPending && questionsThread === PLAN_THREAD;
  const planAttention: Attention = {
    attention: thread !== PLAN_THREAD && (!!unread[PLAN_THREAD] || !!busy[PLAN_THREAD] || planAsked),
    questions: planAsked ? (questions?.questions ?? []).length : 0,
  };
  // Objectives whose lesson and exercise are being written in the background.
  const writing = new Set(Object.keys(busy).flatMap((t) => (busy[t] ? (objectiveOfWriter(t) ?? []) : [])));
  const tutorBusy = Object.keys(busy).some((t) => busy[t] && !objectiveOfWriter(t));

  // "Change with the tutor" on the plan page: the tutor's composer, ready.
  function askTutorAboutPlan() {
    chat.setOpen(true);
    setDraft((d) => (d.trim() ? d : "I'd like to change my plan: "));
    requestAnimationFrame(() => {
      const input = inputRef.current;
      input?.focus();
      input?.setSelectionRange(input.value.length, input.value.length);
    });
  }

  // Until the first plan is approved, everything happens in the centre: the
  // questions, the tutor's work, then the plan to review. No chat beside it.
  const setup = !ws.curriculum;

  const center = setup ? (
    view === "plan" ? (
      <PlanView wsId={id} ws={ws} onClose={() => setView("task")} />
    ) : ws.proposal ? (
      <ProposalView wsId={id} proposal={ws.proposal} current={null} progress={ws.progress} mission={ws.mission} />
    ) : (
      <Setup
        items={threads[PLAN_THREAD] ?? []}
        busy={!!busy[PLAN_THREAD]}
        gate={gate}
        questions={questions}
        lang={lang}
        onAnswer={answerQuestions}
        onDismiss={dismissQuestions}
        onStop={() => App.StopAgent(id, PLAN_THREAD).catch(fail)}
        onSend={(text) => App.SendMessage(id, text, PLAN_THREAD)}
      />
    )
  ) : view === "proposal" && ws.proposal ? (
    <ProposalView wsId={id} proposal={ws.proposal} current={ws.curriculum} progress={ws.progress} />
  ) : view === "plan" ? (
    <PlanView wsId={id} ws={ws} onClose={taskId ? () => setView("task") : undefined} onAskTutor={askTutorAboutPlan} />
  ) : questionsInCenter ? (
    <ReadingPage>
      <PageIntro title="Questions">{questions.intro}</PageIntro>
      <QuestionsForm key={JSON.stringify(questions)} layout="page" form={questions} onAnswer={answerQuestions} onDismiss={dismissQuestions} />
    </ReadingPage>
  ) : todo && objective ? (
    <NotWritten
      key={taskId}
      wsId={id}
      objective={objective}
      part={todo.part}
      moduleTitle={moduleTitle}
      writing={!!busy[writerThread(objective.id)]}
      check={checks[objective.id] ?? null}
      failed={writeFailed[objective.id] ?? ""}
      other={
        todo.part === "lesson"
          ? todoOfKey(exerciseOf(ws, objective.id))
            ? null
            : { label: "Exercise →", onOpen: () => openTask(exerciseOf(ws, objective.id)) }
          : todoOfKey(lessonOf(ws, objective.id))
            ? null
            : { label: "← Lesson", onOpen: () => openTask(lessonOf(ws, objective.id)) }
      }
    />
  ) : openLesson ? (
    <LessonPanel
      key={openLesson.id}
      wsId={id}
      lessonId={openLesson.id}
      moduleTitle={moduleTitle}
      icon={ws.icon}
      lang={lang}
      onOpenExercise={() => openTask(exerciseOf(ws, openLesson.objective))}
      exerciseWritten={!todoOfKey(exerciseOf(ws, openLesson.objective))}
      references={openModule?.references}
    />
  ) : taskId ? (
    <TaskPanel
      key={taskId}
      wsId={id}
      taskId={taskId}
      passed={!!openTaskInfo?.passed}
      moduleTitle={moduleTitle}
      nextTask={open.nextTask}
      moreComing={ws.progress.passed < ws.progress.total}
      // "Next task" leads through the next objective's lesson if it isn't finished.
      onOpenTask={(tid) => openTask(viaLesson(ws, tid))}
    />
  ) : (
    <EmptyState ws={ws} writing={writing.size > 0} />
  );

  const chatQuestions =
    questionsInCenter && thread === PLAN_THREAD ? (
      <div className="max-w-[88%] self-center text-center text-[12px] text-balance wrap-anywhere whitespace-pre-wrap text-muted-foreground">
        Questions are open in the main panel.
      </div>
    ) : questionsPending && questionsThread === thread ? (
      <QuestionsForm key={JSON.stringify(questions)} layout="chat" form={questions} onAnswer={answerQuestions} onDismiss={dismissQuestions} />
    ) : null;

  return (
    <div
      // Curriculum rail, centre, tutor panel (folded into a 44px rail when collapsed).
      className={cn("relative grid h-full", setup ? "grid-cols-[236px_minmax(0,1fr)]" : "grid-cols-[236px_minmax(0,1fr)_var(--chat-w)]")}
      style={setup ? undefined : ({ "--chat-w": chat.open ? `min(${chat.width}px, 50vw)` : "44px" } as CSSProperties)}
    >
      <Sidebar
        ws={ws}
        openTaskId={view === "task" ? taskId : null}
        planOpen={view === "plan"}
        plan={planAttention}
        writing={writing}
        onBack={onBack}
        onOpenTask={openTask}
        onShowPlan={() => setView("plan")}
      />
      <main className="@container flex min-h-0 min-w-0 flex-col overflow-hidden bg-card">
        {ws.proposal && view !== "proposal" && (
          <button
            className="flex flex-none justify-between gap-3 border-b bg-background px-4 py-[9px] text-sm text-soft-foreground"
            onClick={() => setView("proposal")}
          >
            <span>{ws.curriculum ? "Plan changes are waiting for review." : "A plan is waiting for review."}</span>
            <b className="font-medium text-foreground">Review →</b>
          </button>
        )}
        {error && (
          <div className="flex-none border-b px-4 py-2 text-sm text-fail" role="alert">
            {error}
          </div>
        )}
        {center}
      </main>
      {setup ? null : chat.open ? (
        <ChatPanel
          thread={thread}
          context={thread === PLAN_THREAD ? "Plan" : openLesson ? "Lesson" : "Exercise"}
          lang={lang}
          hint={
            openTaskInfo && thread === taskId && !openTaskInfo.passed
              ? {
                  level: Math.min(openTaskInfo.hintLevel + 1, 4),
                  onHint: (level) => App.RequestHint(id, openTaskInfo.id, level).then(refresh).catch(fail),
                }
              : null
          }
          // The setup interview stays out of the tutor panel.
          items={thread === PLAN_THREAD ? afterSetup(threads[PLAN_THREAD] ?? []) : (threads[thread] ?? [])}
          busy={!!busy[thread]}
          questions={chatQuestions}
          status={<StatusLine gate={gate} newTask={newTask} busy={!!busy[thread]} onStop={() => App.StopAgent(id, thread).catch(fail)} onOpenTask={openTask} />}
          draft={draft}
          inputRef={inputRef}
          onDraft={setDraft}
          onSend={(text) => App.SendMessage(id, text, thread).catch(fail)}
          onCollapse={() => chat.setOpen(false)}
          tutor={tutor}
          onSettings={onSettings}
        />
      ) : (
        <ChatRail attention={Object.values(unread).some(Boolean) || questionsPending} busy={tutorBusy} onExpand={() => chat.setOpen(true)} />
      )}
      {!setup && (
        // The handle sits on the tutor panel's left border, in the shell, so one
        // drag can fold the panel into its rail and pull it back out.
        <Splitter
          axis="x"
          className="absolute inset-y-0 right-[calc(var(--chat-w)-1px)] bg-transparent hover:bg-transparent focus-visible:bg-transparent"
          label="Resize the tutor panel (double-click to reset)"
          onDrag={chat.drag}
          onStep={chat.step}
          onReset={chat.reset}
        />
      )}
    </div>
  );
}

/** After the plan is approved, before anything is open. */
function EmptyState({ ws, writing }: { ws: WorkspaceState; writing: boolean }) {
  const title = writing ? "Writing your first lesson…" : "Nothing open";
  const body = writing
    ? "Your lessons and exercises are being written and checked in the sandbox. Each opens here when it's ready."
    : "Pick an objective in the curriculum to start.";
  return (
    <ReadingPage>
      <PageIntro title={title}>{body}</PageIntro>
      {ws.mission.trim() && <Markdown text={ws.mission} className="prose mt-7 border-t pt-[22px]" />}
    </ReadingPage>
  );
}
