import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import {
  afterSetup,
  App,
  errorText,
  exerciseTarget,
  lessonDone,
  lessonKey,
  lessonOfKey,
  lessonTarget,
  mergeChat,
  objectiveOfWriter,
  todoOfKey,
  upsertChat,
  useAppEvents,
  writerThread,
  type ChatItem,
  type GateEvent,
  type QuestionForm,
  type WorkspaceState,
} from "./api";
import { Sidebar } from "./Sidebar";
import { TaskPanel } from "./TaskPanel";
import { LessonPanel } from "./LessonPanel";
import { NotWritten } from "./NotWritten";
import { PlanView } from "./PlanView";
import { ProposalView } from "./ProposalView";
import { ChatPanel, ChatRail, StatusLine, type Attention } from "./ChatPanel";
import { Markdown } from "./Markdown";
import { QuestionsForm } from "./Questions";
import { Setup } from "./Setup";
import { Placeholder, Splitter, clamp, useStored } from "./ui";

export type CenterView = "task" | "plan" | "proposal";

/** Something new the coach wrote while the learner was busy: a task, or a lesson (id is its key). */
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
 * becomes the real lesson or exercise once the coach has written it, and stays
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
  /** The coach as configured in Settings, e.g. "Claude Code · Opus · High". */
  coach: string;
  onSettings: () => void;
  onBack: () => void;
};

export function WorkspaceView({ id, coach, onSettings, onBack }: Props) {
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
  const [gate, setGate] = useState<GateEvent | null>(null);
  const [checks, setChecks] = useState<Record<string, GateEvent | undefined>>({});
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
      setChecks((cur) => newer<GateEvent | undefined>(s.checks ?? {}, cur, "check:"));
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

  useAppEvents((ev) => {
    if (ev.workspace !== id) return;
    switch (ev.kind) {
      case "chat": {
        const item = ev.data;
        const t = item.thread || PLAN;
        // Writers aren't chats; only their failures matter here.
        const writing = objectiveOfWriter(t);
        if (writing) {
          if (item.status === "failed") setWriteFailed((f) => ({ ...f, [writing]: item.text }));
          break;
        }
        setThreads((all) => ({ ...all, [t]: upsertChat(all[t] ?? [], item) }));
        if (t !== thread || !chatOpen) setUnread((u) => (u[t] ? u : { ...u, [t]: true }));
        break;
      }
      case "busy": {
        const b = ev.data;
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
        break;
      }
      case "changed":
        refresh();
        break;
      case "questions":
        setQuestions(ev.data);
        break;
      case "proposal":
        refresh().then((s) => s?.proposal && setView("proposal"));
        break;
      case "gate": {
        const g = ev.data;
        if (g.objective) {
          changed(`check:${g.objective}`);
          setChecks((c) => ({ ...c, [g.objective!]: g }));
        } else setGate(g);
        break;
      }
      case "task": {
        const info = ev.data;
        refresh();
        if (todo?.part === "exercise" && todo.objective === info.objective) {
          openTask(info.id);
        } else if (!taskId) {
          openTask(info.id);
        } else if (ws?.tasks?.find((t) => t.id === taskId)?.passed) {
          // Tasks are written ahead in the background; only point at a new one
          // once the learner is done with the open task. Its lesson, if
          // announced, comes first.
          setNewTask((cur) => (cur?.kind === "lesson" ? cur : { id: info.id, title: info.title, kind: "task" }));
        }
        break;
      }
      case "lesson": {
        const info = ev.data;
        refresh();
        if (todo?.part === "lesson" && todo.objective === info.objective) {
          openTask(lessonKey(info.id));
        } else if (!taskId) {
          openTask(lessonKey(info.id));
        } else if (ws?.tasks?.find((t) => t.id === taskId)?.passed) {
          setNewTask({
            id: lessonKey(info.id),
            title: info.title,
            kind: "lesson",
          });
        }
        break;
      }
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
        <button className="btn" onClick={onBack}>
          Back to workspaces
        </button>
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
  const lessonOf = (o: string) => lessonTarget(o, lessons.find((l) => l.objective === o));
  const exerciseOf = (o: string) => exerciseTarget(o, tasks.filter((t) => t.objective === o));

  // The next unsolved task, preferring ones after the open task.
  const openTaskInfo = tasks.find((t) => t.id === taskId) ?? null;
  const openIndex = tasks.findIndex((t) => t.id === taskId);
  const nextTask = tasks.slice(openIndex + 1).find((t) => !t.passed) ?? tasks.find((t) => !t.passed && t.id !== taskId) ?? null;

  // Before the first task, the coach's questions are the main thing to do, so
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
      <div className="reading">
        <div className="empty-intro">
          <h1>Questions</h1>
          {questions.intro && <p>{questions.intro}</p>}
        </div>
        <QuestionsForm key={JSON.stringify(questions)} layout="page" form={questions} onAnswer={answer} onDismiss={dismissQuestions} />
      </div>
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
      <div className="msg-notice">Questions are open in the main panel.</div>
    ) : questionsPending && questionsThread === thread ? (
      <QuestionsForm key={JSON.stringify(questions)} layout="chat" form={questions} onAnswer={answer} onDismiss={dismissQuestions} />
    ) : null;

  return (
    <div
      className={`ws ${setup ? "no-chat" : chatOpen ? "" : "chat-collapsed"}`}
      style={chatOpen && !setup ? ({ "--chat-w": `min(${chatWidth}px, 50vw)` } as CSSProperties) : undefined}
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
      <main className="center">
        {ws.proposal && view !== "proposal" && (
          <button className="proposal-banner" onClick={() => setView("proposal")}>
            <span>{ws.curriculum ? "Plan changes are waiting for review." : "A plan is waiting for review."}</span>
            <b>Review →</b>
          </button>
        )}
        {loadError && (
          <div className="banner-error" role="alert">
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
          coach={coach}
          onSettings={onSettings}
        />
      ) : (
        <ChatRail attention={Object.values(unread).some(Boolean) || questionsPending} busy={tutorBusy} onExpand={() => setChatOpen(true)} />
      )}
      {!setup && (
        <Splitter
          axis="x"
          className="chat-resizer"
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
    <div className="reading">
      <div className="empty-intro">
        <h1>{title}</h1>
        <p>{body}</p>
      </div>
      {ws.mission.trim() && <Markdown text={ws.mission} className="prose mission-inline" />}
    </div>
  );
}
