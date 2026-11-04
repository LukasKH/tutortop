import type { CSSProperties } from "react";
import { cn } from "cn";
import { afterSetup } from "@/lib/chat";
import { App, type ChatItem } from "@/lib/api";
import { PLAN_THREAD } from "@/lib/workspace";
import { describeItem, type OpenItem } from "@/features/workspace/open-item";
import { useChatPanel } from "@/features/workspace/use-chat-panel";
import { useChatThreads } from "@/features/workspace/use-chat-threads";
import { centerOf, placeQuestions, useOpenItem, type Center } from "@/features/workspace/use-open-item";
import { useWorkspace, type UseWorkspaceResult } from "@/features/workspace/use-workspace";
import { Sidebar } from "@/features/workspace/sidebar";
import { TaskPanel } from "@/features/task/task-panel";
import { LessonPanel } from "@/features/lesson/lesson-panel";
import { NotWritten } from "@/features/workspace/not-written";
import { PlanView } from "@/features/workspace/plan-view";
import { ProposalView } from "@/features/workspace/proposal-view";
import { ChatPanel, ChatRail, StatusLine, type Attention } from "@/features/chat/chat-panel";
import { useComposer } from "@/features/chat/use-composer";
import { Markdown } from "@/components/markdown";
import { QuestionsForm } from "@/features/workspace/questions";
import { Setup } from "@/features/workspace/setup";
import { Placeholder } from "@/components/placeholder";
import { PageIntro, ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { Splitter } from "@/components/splitter";

type Props = {
  id: string;
  /** The tutor as configured in Settings, e.g. "Claude Code · Opus · High". */
  tutor: string;
  onSettings: () => void;
  onBack: () => void;
};

/** A workspace: the curriculum rail, the open lesson, exercise or plan page, and the tutor beside it. */
export function WorkspaceView({ id, tutor, onSettings, onBack }: Props) {
  // The backend's state, kept current by events (use-workspace.ts).
  const live = useWorkspace(id);
  // What the learner has open, and the chat thread that goes with it (use-open-item.ts).
  const nav = useOpenItem(id, live);
  // The chat panel: open or folded into its rail, its width, and its message box.
  const chat = useChatPanel();
  const composer = useComposer();
  // Conversations by thread, loaded for the shown thread and streamed in by event.
  const { threads, unread } = useChatThreads(id, nav.thread, { ready: live.ws !== null, visible: chat.open, onError: live.fail });

  const { ws, questions, fail } = live;
  if (!ws) {
    return (
      <Placeholder error={live.error} loading="Opening workspace…">
        <Button variant="outline" onClick={onBack}>
          Back to workspaces
        </Button>
      </Placeholder>
    );
  }

  const { item, view, thread } = nav;
  // The shown chat's tutor is working: its "Thinking…", Stop and hint button follow this.
  const threadBusy = live.isBusy(thread);
  // The open item's task or lesson, module and language, for the centre and the chat.
  const details = describeItem(ws, item);
  // Until the first plan is approved, everything happens in the centre. No chat beside it.
  const setup = !ws.curriculum;

  // The tutor's questions: in the centre, or in the chat of the thread that asked.
  const place = placeQuestions(ws, view, item, thread, questions);
  const center = centerOf(ws, view, details, place.center ? questions : null);
  const planAttention: Attention = {
    attention: thread !== PLAN_THREAD && (!!unread[PLAN_THREAD] || live.isBusy(PLAN_THREAD) || place.planBadge > 0),
    questions: place.planBadge,
  };

  // Hints are for the open exercise's own conversation, until it's solved.
  const hintTask = details.task && thread === details.task.id && !details.task.passed ? details.task : null;

  // "Change with the tutor" on the plan page: the tutor's composer, ready.
  function askTutorAboutPlan() {
    chat.setOpen(true);
    composer.prefill("I'd like to change my plan: ");
  }

  return (
    <div
      // Curriculum rail, centre, chat panel (folded into a 44px rail when collapsed).
      className={cn("relative grid h-full", setup ? "grid-cols-[236px_minmax(0,1fr)]" : "grid-cols-[236px_minmax(0,1fr)_var(--chat-w)]")}
      style={setup ? undefined : ({ "--chat-w": chat.open ? `min(${chat.width}px, 50vw)` : "44px" } as CSSProperties)}
    >
      <Sidebar
        ws={ws}
        openItem={view === "item" ? item : null}
        planOpen={view === "plan"}
        plan={planAttention}
        writing={live.writing}
        onBack={onBack}
        onOpen={nav.open}
        onShowPlan={() => nav.setView("plan")}
      />
      <main className="@container flex min-h-0 min-w-0 flex-col overflow-hidden bg-card">
        {ws.proposal && view !== "proposal" && (
          <button
            className="flex flex-none justify-between gap-3 border-b bg-background px-4 py-[9px] text-sm text-soft-foreground"
            onClick={() => nav.setView("proposal")}
          >
            <span>{ws.curriculum ? "Plan changes are waiting for review." : "A plan is waiting for review."}</span>
            <b className="font-medium text-foreground">Review →</b>
          </button>
        )}
        {live.error && (
          <div className="flex-none border-b px-4 py-2 text-sm text-fail" role="alert">
            {live.error}
          </div>
        )}
        <CenterPane
          id={id}
          center={center}
          live={live}
          planItems={threads[PLAN_THREAD] ?? []}
          onOpen={nav.open}
          onBack={() => nav.setView("item")}
          onAskTutor={askTutorAboutPlan}
        />
      </main>
      {setup ? null : chat.open ? (
        <ChatPanel
          thread={thread}
          context={thread === PLAN_THREAD ? "Plan" : item?.kind === "lesson" ? "Lesson" : "Exercise"}
          lang={details.lang}
          hint={
            hintTask && {
              level: Math.min(hintTask.hintLevel + 1, 4),
              onHint: (level) => App.RequestHint(id, hintTask.id, level).then(live.refresh).catch(fail),
            }
          }
          // The setup interview stays out of the chat panel.
          items={thread === PLAN_THREAD ? afterSetup(threads[PLAN_THREAD] ?? []) : (threads[thread] ?? [])}
          busy={threadBusy}
          questions={
            questions && place.chat === thread ? (
              <QuestionsForm key={JSON.stringify(questions)} layout="chat" form={questions} onAnswer={live.answerQuestions} onDismiss={live.dismissQuestions} />
            ) : place.center ? (
              <div className="max-w-[88%] self-center text-center text-[12px] text-balance wrap-anywhere whitespace-pre-wrap text-muted-foreground">
                Questions are open in the main panel.
              </div>
            ) : null
          }
          status={
            <StatusLine
              check={live.toolchainCheck}
              newItem={nav.newItem}
              busy={threadBusy}
              onStop={() => App.StopAgent(id, thread).catch(fail)}
              onOpen={nav.open}
            />
          }
          composer={composer}
          onSend={(text) => App.SendMessage(id, text, thread).catch(fail)}
          onCollapse={() => chat.setOpen(false)}
          tutor={tutor}
          onSettings={onSettings}
        />
      ) : (
        <ChatRail attention={Object.values(unread).some(Boolean) || place.chat !== null} busy={live.tutorBusy} onExpand={() => chat.setOpen(true)} />
      )}
      {!setup && (
        // The handle sits on the chat panel's left border, in the shell, so one
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

type CenterProps = {
  id: string;
  /** What to show, with what it shows from the snapshot (centerOf). */
  center: Center;
  /** Who is working, writers' checks and failures, and the calls (useWorkspace). The snapshot's part comes in center. */
  live: UseWorkspaceResult;
  /** The planning conversation, which setup shows in the centre. */
  planItems: ChatItem[];
  onOpen: (item: OpenItem) => void;
  /** Leaves a plan page for the open item. */
  onBack: () => void;
  onAskTutor: () => void;
};

/** The centre pane, as centerOf decided. */
function CenterPane({ id, center, live, planItems, onOpen, onBack, onAskTutor }: CenterProps) {
  switch (center.kind) {
    case "setup":
      return (
        <Setup
          items={planItems}
          busy={live.isBusy(PLAN_THREAD)}
          check={live.toolchainCheck}
          questions={center.questions}
          lang={center.lang}
          onAnswer={live.answerQuestions}
          onDismiss={live.dismissQuestions}
          onStop={() => App.StopAgent(id, PLAN_THREAD).catch(live.fail)}
          onSend={(text) => App.SendMessage(id, text, PLAN_THREAD)}
        />
      );

    case "proposal":
      return <ProposalView wsId={id} proposal={center.proposal} current={center.current} progress={center.progress} mission={center.mission} />;

    case "plan":
      return <PlanView wsId={id} ws={center.ws} onClose={center.canGoBack ? onBack : undefined} onAskTutor={center.canAskTutor ? onAskTutor : undefined} />;

    case "questions":
      return (
        <ReadingPage>
          <PageIntro title="Questions">{center.questions.intro}</PageIntro>
          <QuestionsForm
            key={JSON.stringify(center.questions)}
            layout="page"
            form={center.questions}
            onAnswer={live.answerQuestions}
            onDismiss={live.dismissQuestions}
          />
        </ReadingPage>
      );

    case "not-written": {
      const { objective, part, other } = center;
      return (
        <NotWritten
          key={`${part}:${objective.id}`}
          wsId={id}
          objective={objective}
          part={part}
          moduleTitle={center.moduleTitle}
          writing={live.writing.has(objective.id)}
          check={live.checks[objective.id] ?? null}
          failed={live.writeFailed[objective.id] ?? ""}
          other={other && { label: part === "lesson" ? "Exercise →" : "← Lesson", onOpen: () => onOpen(other) }}
        />
      );
    }

    case "lesson":
      return (
        <LessonPanel
          key={center.lesson.id}
          wsId={id}
          lessonId={center.lesson.id}
          moduleTitle={center.moduleTitle}
          icon={center.icon}
          lang={center.lang}
          onOpenExercise={() => onOpen(center.exercise)}
          exerciseWritten={center.exercise.kind !== "todo"}
          references={center.references}
        />
      );

    case "task": {
      const { openNext } = center;
      return (
        <TaskPanel
          key={center.id}
          wsId={id}
          taskId={center.id}
          passed={center.passed}
          moduleTitle={center.moduleTitle}
          nextTask={center.nextTask}
          moreComing={center.moreComing}
          onOpenNext={() => openNext && onOpen(openNext)}
        />
      );
    }

    case "empty":
      return <EmptyState mission={center.mission} writing={live.writing.size > 0} />;
  }
}

/** After the plan is approved, before anything is open. */
function EmptyState({ mission, writing }: { mission: string; writing: boolean }) {
  const title = writing ? "Writing your first lesson…" : "Nothing open";
  const body = writing
    ? "Your lessons and exercises are being written and checked in the sandbox. Each opens here when it's ready."
    : "Pick an objective in the curriculum to start.";
  return (
    <ReadingPage>
      <PageIntro title={title}>{body}</PageIntro>
      {mission.trim() && <Markdown text={mission} className="prose mt-7 border-t pt-[22px]" />}
    </ReadingPage>
  );
}
