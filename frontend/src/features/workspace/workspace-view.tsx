import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { cn } from "cn";
import { afterSetup, mergeChat, upsertChat } from "@/lib/chat";
import { App, errorText, AppEvents, useEvent, type ChatItem, type Gate, type QuestionForm, type WorkspaceState } from "@/lib/api";
import { exerciseTarget, lessonDone, lessonKey, lessonOfKey, lessonTarget, objectiveOfWriter, todoOfKey, writerThread } from "@/lib/workspace";
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
import { Splitter, clamp } from "@/components/splitter";
import { useStored } from "@/hooks/use-stored";

export type CenterView = "task" | "plan" | "proposal";

/** Something new the tutor wrote while the learner was busy: a task, or a lesson (id is its key). */
export type NewItem = { id: string; title: string; kind: "task" | "lesson" };

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

/**
 * Keeps the open item if it still exists. Something that wasn't written yet
 * becomes the real lesson or exercise once the tutor has written it, and stays
 * open while its objective is in the plan.
 */
function resolveOpen(s: WorkspaceState, cur: string | null): string | null {
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

const PLAN = "plan";

// The chat panel's width: dragged between CHAT_MIN and half the window;
// dragging well below the minimum folds it into the rail.
const CHAT_MIN = 300;
const CHAT_FOLD = 220;
const chatDefault = () => (window.innerWidth >= 1440 ? 380 : 340);

type Props = {
  id: string;
  /** The tutor as configured in Settings, e.g. "Claude Code · Opus · High". */
  tutor: string;
  onSettings: () => void;
  onBack: () => void;
};

export function WorkspaceView({ id, tutor, onSettings, onBack }: Props) {
  const [ws, setWs] = useState<WorkspaceState | null>(null);
  const [loadError, setLoadError] = useState("");
  // Conversations by thread: "plan", or a task id. Kept in memory so live
  // (still streaming) items survive switching between threads.
  const [threads, setThreads] = useState<Record<string, ChatItem[]>>({});
  const [busy, setBusy] = useState<Record<string, boolean | undefined>>({});
  const [unread, setUnread] = useState<Record<string, boolean>>({});
  const [questions, setQuestions] = useState<QuestionForm | null>(null);
  // The planning tutor's sandbox check (a new language), shown in the chat's
  // status line. Writers' checks of new exercises show on their objective.
  const [gate, setGate] = useState<Gate | null>(null);
  const [checks, setChecks] = useState<Record<string, Gate | undefined>>({});
  // Why an objective's writer stopped, if it failed.
  const [writeFailed, setWriteFailed] = useState<Record<string, string | undefined>>({});
  const [newTask, setNewTask] = useState<NewItem | null>(null);
  // The open task or lesson (by key), remembered per workspace. refresh() checks it still exists.
  const [taskId, setTaskId] = useStored<string | null>(`task.${id}`, null);
  const [view, setView] = useState<CenterView>("task");
  const [draft, setDraft] = useState("");
  const [chatOpen, setChatOpen] = useStored("chat.open", true);
  const [chatWidth, setChatWidth] = useStored("chat.width", chatDefault);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fail = (e: unknown) => setLoadError(errorText(e));

  // The chat follows the centre pane: an open exercise or lesson has its own
  // conversation; the plan page, plan review, and anything not written yet
  // use the planning conversation. Writing happens apart from the chat. Until the
  // workspace loads, the stored id is unchecked.
  const todo = todoOfKey(taskId);
  const hasItem = !!taskId && !todo;
  const planContext = view !== "task" || !hasItem;
  const thread = ws && !planContext ? taskId! : PLAN;

  // When each thread's busy flag and each objective's check last changed by
  // event. A snapshot requested before that is older, so it doesn't undo it:
  // otherwise a writer that finishes during a refetch would spin forever.
  const changedAt = useRef<Record<string, number>>({});
  const changed = (key: string) => (changedAt.current[key] = performance.now());

  // Refetches can overlap during a burst of events; only the newest one wins.
  // Whichever refresh lands first does the first-load choices, so a fast
  // event can't skip them.
  const seq = useRef(0);
  const loaded = useRef(false);
  const refresh = useCallback(async () => {
    const mine = ++seq.current;
    const asked = performance.now();
    // Keep what events said after the snapshot was asked for.
    const newer = <T,>(snapshot: Record<string, T>, current: Record<string, T>, prefix: string) => {
      const next = { ...snapshot };
      for (const [key, at] of Object.entries(changedAt.current)) {
        if (at > asked && key.startsWith(prefix)) next[key.slice(prefix.length)] = current[key.slice(prefix.length)];
      }
      return next;
    };
    try {
      const s = await App.Workspace(id);
      if (mine !== seq.current) return null;
      setWs(s);
      setBusy((cur) => newer<boolean | undefined>(s.busy ?? {}, cur, "busy:"));
      setChecks((cur) => newer<Gate | undefined>(s.checks ?? {}, cur, "check:"));
      setQuestions(s.questions ?? null);
      setLoadError("");
      setTaskId((cur) => resolveOpen(s, cur));
      if (!loaded.current) {
        loaded.current = true;
        if (s.proposal) setView("proposal");
      }
      return s;
    } catch (e) {
      fail(e);
      return null;
    }
  }, [id]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Load the visible thread's transcript whenever it changes, merged with
  // anything that arrived live and isn't saved yet.
  const ready = ws !== null;
  useEffect(() => {
    if (!ready) return;
    let current = true;
    App.Chat(id, thread)
      .then(
        (saved) =>
          current &&
          setThreads((t) => ({
            ...t,
            [thread]: mergeChat(saved, t[thread] ?? []),
          })),
      )
      .catch((e) => current && fail(e));
    return () => {
      current = false;
    };
  }, [id, thread, ready]);

  // What the learner can see counts as read.
  useEffect(() => {
    if (chatOpen) setUnread((u) => (u[thread] ? { ...u, [thread]: false } : u));
  }, [thread, chatOpen]);

  useEvent(AppEvents.ChatMessage, ({ workspace, item }) => {
    if (workspace !== id) return;
    const t = item.thread || PLAN;
    // Writers aren't chats; only their failures matter here.
    const writing = objectiveOfWriter(t);
    if (writing) {
      if (item.status === "failed") setWriteFailed((f) => ({ ...f, [writing]: item.text }));
      return;
    }
    setThreads((all) => ({ ...all, [t]: upsertChat(all[t] ?? [], item) }));
    if (t !== thread || !chatOpen) setUnread((u) => (u[t] ? u : { ...u, [t]: true }));
  });

  useEvent(AppEvents.ThreadBusy, (b) => {
    if (b.workspace !== id) return;
    const t = b.thread || PLAN;
    changed(`busy:${t}`);
    setBusy((all) => ({ ...all, [t]: b.busy }));
    // A writer starting or finishing starts its objective's status afresh.
    const writing = objectiveOfWriter(t);
    if (writing) {
      changed(`check:${writing}`);
      setChecks((c) => ({ ...c, [writing]: undefined }));
      if (b.busy) setWriteFailed((f) => ({ ...f, [writing]: undefined }));
    }
    // A finished gate result is only interesting while the plan thread works on it.
    if (t === PLAN && !b.busy) setGate((g) => (g?.running ? g : null));
  });

  useEvent(AppEvents.WorkspaceChanged, ({ workspace }) => {
    if (workspace === id) refresh();
  });

  useEvent(AppEvents.QuestionsAsked, ({ workspace, questions }) => {
    if (workspace === id) setQuestions(questions);
  });

  useEvent(AppEvents.CurriculumProposed, ({ workspace }) => {
    if (workspace === id) refresh().then((s) => s?.proposal && setView("proposal"));
  });

  useEvent(AppEvents.GateStatus, ({ workspace, gate: g }) => {
    if (workspace !== id) return;
    if (g.objective) {
      changed(`check:${g.objective}`);
      setChecks((c) => ({ ...c, [g.objective!]: g }));
    } else setGate(g);
  });

  useEvent(AppEvents.TaskPublished, ({ workspace, task }) => {
    if (workspace !== id) return;
    refresh();
    if (todo?.part === "exercise" && todo.objective === task.objective) {
      openTask(task.id);
    } else if (!taskId) {
      openTask(task.id);
    } else if (ws?.tasks?.find((t) => t.id === taskId)?.passed) {
      // Tasks are written ahead in the background; only point at a new one
      // once the learner is done with the open task. Its lesson, if
      // announced, comes first.
      setNewTask((cur) => (cur?.kind === "lesson" ? cur : { id: task.id, title: task.title, kind: "task" }));
    }
  });

  useEvent(AppEvents.LessonPublished, ({ workspace, lesson }) => {
    if (workspace !== id) return;
    refresh();
    if (todo?.part === "lesson" && todo.objective === lesson.objective) {
      openTask(lessonKey(lesson.id));
    } else if (!taskId) {
      openTask(lessonKey(lesson.id));
    } else if (ws?.tasks?.find((t) => t.id === taskId)?.passed) {
      setNewTask({
        id: lessonKey(lesson.id),
        title: lesson.title,
        kind: "lesson",
      });
    }
  });

  function openTask(tid: string) {
    setTaskId(tid);
    setView("task");
    setNewTask((t) => (t?.id === tid ? null : t));
    setGate((g) => (g && !g.running ? null : g));
  }

  async function answer(answers: Record<string, string>) {
    await App.AnswerQuestions(id, answers);
    setQuestions(null);
  }

  function dismissQuestions() {
    App.DismissQuestions(id).catch(fail);
    setQuestions(null);
  }

  // One bounded setter for the chat width, shared by dragging and the keyboard.
  const sizeChat = (width: number) => setChatWidth(Math.round(clamp(width, CHAT_MIN, window.innerWidth / 2)));
  function dragChat(x: number) {
    const width = window.innerWidth - x;
    if (width < CHAT_FOLD) return chatOpen && setChatOpen(false);
    if (!chatOpen) setChatOpen(true);
    sizeChat(width);
  }

  if (!ws) {
    return (
      <Placeholder error={loadError} loading="Opening workspace…">
        <Button variant="outline" onClick={onBack}>
          Back to workspaces
        </Button>
      </Placeholder>
    );
  }

  // The task bar shows the module of the open task's (or lesson's) objective.
  const openLessonId = lessonOfKey(taskId);
  const lessons = ws.lessons ?? [];
  const openLesson = lessons.find((l) => l.id === openLessonId) ?? null;
  const openObjective = todo?.objective ?? openLesson?.objective ?? (ws.tasks ?? []).find((t) => t.id === taskId)?.objective;
  const openModule = (ws.curriculum?.modules ?? []).find((m) => (m.objectives ?? []).some((o) => o.id === openObjective));
  const moduleTitle = openModule?.title ?? "";
  const objective = openModule?.objectives?.find((o) => o.id === openObjective) ?? null;
  // The language code is coloured in: the open exercise's, else its objective's, else the plan's.
  const lang = (ws.tasks ?? []).find((t) => t.id === taskId)?.language || objective?.language || ws.curriculum?.language || "";
  // Every objective has a lesson and an exercise to open, written or not.
  const tasks = ws.tasks ?? [];
  const lessonOf = (o: string) =>
    lessonTarget(
      o,
      lessons.find((l) => l.objective === o),
    );
  const exerciseOf = (o: string) =>
    exerciseTarget(
      o,
      tasks.filter((t) => t.objective === o),
    );

  // The next unsolved task, preferring ones after the open task.
  const openTaskInfo = tasks.find((t) => t.id === taskId) ?? null;
  const openIndex = tasks.findIndex((t) => t.id === taskId);
  const nextTask = tasks.slice(openIndex + 1).find((t) => !t.passed) ?? tasks.find((t) => !t.passed && t.id !== taskId) ?? null;

  // Before the first task, the tutor's questions are the main thing to do, so
  // they get the centre pane instead of the narrow chat column. Otherwise they
  // appear in the chat of the thread that asked.
  const questionsThread = questions?.thread || PLAN;
  const questionsInCenter = !!questions && !taskId && view === "task";
  const questionsPending = !!questions && !questionsInCenter;
  const planAsked = questionsPending && questionsThread === PLAN;
  const planAttention: Attention = {
    attention: thread !== PLAN && (!!unread[PLAN] || !!busy[PLAN] || planAsked),
    questions: planAsked ? (questions?.questions ?? []).length : 0,
  };
  const planChat = threads[PLAN] ?? [];
  // Objectives whose lesson and exercise are being written in the background.
  const writing = new Set(Object.keys(busy).flatMap((t) => (busy[t] ? (objectiveOfWriter(t) ?? []) : [])));
  const tutorBusy = Object.keys(busy).some((t) => busy[t] && !objectiveOfWriter(t));

  // "Change with the tutor" on the plan page: the tutor's composer, ready.
  function askTutorAboutPlan() {
    setChatOpen(true);
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
        items={planChat}
        busy={!!busy[PLAN]}
        gate={gate}
        questions={questions}
        lang={lang}
        onAnswer={answer}
        onDismiss={dismissQuestions}
        onStop={() => App.StopAgent(id, PLAN).catch(fail)}
        onSend={(text) => App.SendMessage(id, text, PLAN)}
      />
    )
  ) : view === "proposal" && ws.proposal ? (
    <ProposalView wsId={id} proposal={ws.proposal} current={ws.curriculum} progress={ws.progress} />
  ) : view === "plan" ? (
    <PlanView wsId={id} ws={ws} onClose={taskId ? () => setView("task") : undefined} onAskTutor={askTutorAboutPlan} />
  ) : questionsInCenter ? (
    <ReadingPage>
      <PageIntro title="Questions">{questions.intro}</PageIntro>
      <QuestionsForm key={JSON.stringify(questions)} layout="page" form={questions} onAnswer={answer} onDismiss={dismissQuestions} />
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
          ? todoOfKey(exerciseOf(objective.id))
            ? null
            : {
                label: "Exercise →",
                onOpen: () => openTask(exerciseOf(objective.id)),
              }
          : todoOfKey(lessonOf(objective.id))
            ? null
            : {
                label: "← Lesson",
                onOpen: () => openTask(lessonOf(objective.id)),
              }
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
      onOpenExercise={() => openTask(exerciseOf(openLesson.objective))}
      exerciseWritten={!todoOfKey(exerciseOf(openLesson.objective))}
      references={openModule?.references}
    />
  ) : taskId ? (
    <TaskPanel
      key={taskId}
      wsId={id}
      taskId={taskId}
      passed={!!tasks.find((t) => t.id === taskId)?.passed}
      moduleTitle={moduleTitle}
      nextTask={nextTask}
      moreComing={ws.progress.passed < ws.progress.total}
      // "Next task" leads through the next objective's lesson if it isn't finished.
      onOpenTask={(tid) => {
        const o = tasks.find((t) => t.id === tid)?.objective;
        const l = lessons.find((x) => x.objective === o && !lessonDone(x));
        openTask(l ? lessonKey(l.id) : tid);
      }}
    />
  ) : (
    <EmptyState ws={ws} writing={writing.size > 0} />
  );

  const chatQuestions =
    questionsInCenter && thread === PLAN ? (
      <div className="max-w-[88%] self-center text-center text-[12px] text-balance wrap-anywhere whitespace-pre-wrap text-muted-foreground">
        Questions are open in the main panel.
      </div>
    ) : questionsPending && questionsThread === thread ? (
      <QuestionsForm key={JSON.stringify(questions)} layout="chat" form={questions} onAnswer={answer} onDismiss={dismissQuestions} />
    ) : null;

  return (
    <div
      // Curriculum rail, centre, tutor panel (folded into a 44px rail when collapsed).
      className={cn("relative grid h-full", setup ? "grid-cols-[236px_minmax(0,1fr)]" : "grid-cols-[236px_minmax(0,1fr)_var(--chat-w)]")}
      style={setup ? undefined : ({ "--chat-w": chatOpen ? `min(${chatWidth}px, 50vw)` : "44px" } as CSSProperties)}
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
        {loadError && (
          <div className="flex-none border-b px-4 py-2 text-sm text-fail" role="alert">
            {loadError}
          </div>
        )}
        {center}
      </main>
      {setup ? null : chatOpen ? (
        <ChatPanel
          thread={thread}
          context={thread === PLAN ? "Plan" : openLesson ? "Lesson" : "Exercise"}
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
          items={thread === PLAN ? afterSetup(threads[PLAN] ?? []) : (threads[thread] ?? [])}
          busy={!!busy[thread]}
          questions={chatQuestions}
          status={<StatusLine gate={gate} newTask={newTask} busy={!!busy[thread]} onStop={() => App.StopAgent(id, thread).catch(fail)} onOpenTask={openTask} />}
          draft={draft}
          inputRef={inputRef}
          onDraft={setDraft}
          onSend={(text) => App.SendMessage(id, text, thread).catch(fail)}
          onCollapse={() => setChatOpen(false)}
          tutor={tutor}
          onSettings={onSettings}
        />
      ) : (
        <ChatRail attention={Object.values(unread).some(Boolean) || questionsPending} busy={tutorBusy} onExpand={() => setChatOpen(true)} />
      )}
      {!setup && (
        // The handle sits on the tutor panel's left border, in the shell, so one
        // drag can fold the panel into its rail and pull it back out.
        <Splitter
          axis="x"
          className="absolute inset-y-0 right-[calc(var(--chat-w)-1px)] bg-transparent hover:bg-transparent focus-visible:bg-transparent"
          label="Resize the tutor panel (double-click to reset)"
          onDrag={dragChat}
          onStep={(d) => (chatOpen ? sizeChat(chatWidth - d * 24) : setChatOpen(true))}
          onReset={() => {
            setChatOpen(true);
            setChatWidth(chatDefault());
          }}
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
