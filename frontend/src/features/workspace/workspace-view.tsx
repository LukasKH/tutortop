import type { CSSProperties } from "react";
import { cn } from "cn";
import { afterSetup } from "@/lib/chat";
import { App, type ChatItem, type WorkspaceState } from "@/lib/api";
import { PLAN_THREAD, writerThread } from "@/lib/workspace";
import { describeItem, exerciseOf, itemKey, lessonOf, viaLesson } from "@/features/workspace/open-item";
import { useChatPanel } from "@/features/workspace/use-chat-panel";
import { useChatThreads } from "@/features/workspace/use-chat-threads";
import { centerOf, questionsInCenter, useOpenItem, type Center } from "@/features/workspace/use-open-item";
import { useWorkspace, type Workspace } from "@/features/workspace/use-workspace";
import { Sidebar } from "@/features/workspace/sidebar";
import { TaskPanel } from "@/features/task/task-panel";
import { LessonPanel } from "@/features/lesson/lesson-panel";
import { NotWritten } from "@/features/workspace/not-written";
import { PlanView } from "@/features/workspace/plan-view";
import { ProposalView } from "@/features/workspace/proposal-view";
import { ChatPanel, ChatRail, StatusLine, type Attention } from "@/features/tutor/chat-panel";
import { useComposer } from "@/features/tutor/use-composer";
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
  // The tutor panel: open or folded into its rail, its width, and its message box.
  const chat = useChatPanel();
  const composer = useComposer();
  // Conversations by thread, loaded for the shown thread and streamed in by event.
  const { threads, unread } = useChatThreads(id, nav.thread, { ready: live.ws !== null, visible: chat.open, onError: live.fail });

  const { ws, busy, questions, fail } = live;
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
  // The open item's task or lesson, module and language.
  const details = describeItem(ws, item);
  // Until the first plan is approved, everything happens in the centre. No chat beside it.
  const setup = !ws.curriculum;

  // Questions that aren't in the centre wait in the chat of the thread that
  // asked. The plan's also show on "Your plan" while another chat is shown.
  const questionsWaiting = !!questions && !questionsInCenter(view, item, questions);
  const planAsked = questionsWaiting && questions.thread === PLAN_THREAD;
  const planAttention: Attention = {
    attention: thread !== PLAN_THREAD && (!!unread[PLAN_THREAD] || !!busy[PLAN_THREAD] || planAsked),
    questions: planAsked ? (questions?.questions ?? []).length : 0,
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
      // Curriculum rail, centre, tutor panel (folded into a 44px rail when collapsed).
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
          center={centerOf(ws, view, item, questions)}
          ws={ws}
          live={live}
          nav={nav}
          planItems={threads[PLAN_THREAD] ?? []}
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
          // The setup interview stays out of the tutor panel.
          items={thread === PLAN_THREAD ? afterSetup(threads[PLAN_THREAD] ?? []) : (threads[thread] ?? [])}
          busy={!!busy[thread]}
          questions={
            questions && !questionsWaiting && thread === PLAN_THREAD ? (
              <div className="max-w-[88%] self-center text-center text-[12px] text-balance wrap-anywhere whitespace-pre-wrap text-muted-foreground">
                Questions are open in the main panel.
              </div>
            ) : questions && questionsWaiting && questions.thread === thread ? (
              <QuestionsForm key={JSON.stringify(questions)} layout="chat" form={questions} onAnswer={live.answerQuestions} onDismiss={live.dismissQuestions} />
            ) : null
          }
          status={
            <StatusLine
              check={live.toolchainCheck}
              newItem={nav.newItem}
              busy={!!busy[thread]}
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
        <ChatRail attention={Object.values(unread).some(Boolean) || questionsWaiting} busy={live.tutorBusy} onExpand={() => chat.setOpen(true)} />
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

type CenterProps = {
  id: string;
  center: Center;
  ws: WorkspaceState;
  live: Workspace;
  nav: ReturnType<typeof useOpenItem>;
  /** The planning conversation, which setup shows in the centre. */
  planItems: ChatItem[];
  onAskTutor: () => void;
};

/** The centre pane, as centerOf decided. */
function CenterPane({ id, center, ws, live, nav, planItems, onAskTutor }: CenterProps) {
  const { item } = nav;
  const details = describeItem(ws, item);
  const moduleTitle = details.module?.title ?? "";
  const back = () => nav.setView("item");

  switch (center.kind) {
    case "setup":
      return (
        <Setup
          items={planItems}
          busy={!!live.busy[PLAN_THREAD]}
          check={live.toolchainCheck}
          questions={live.questions}
          lang={details.lang}
          onAnswer={live.answerQuestions}
          onDismiss={live.dismissQuestions}
          onStop={() => App.StopAgent(id, PLAN_THREAD).catch(live.fail)}
          onSend={(text) => App.SendMessage(id, text, PLAN_THREAD)}
        />
      );

    case "proposal":
      return <ProposalView wsId={id} proposal={center.proposal} current={ws.curriculum} progress={ws.progress} mission={ws.mission} />;

    case "plan":
      // Before the first plan there's no tutor panel to ask; after it, "Back" needs something open.
      return ws.curriculum ? (
        <PlanView wsId={id} ws={ws} onClose={item ? back : undefined} onAskTutor={onAskTutor} />
      ) : (
        <PlanView wsId={id} ws={ws} onClose={back} />
      );

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
      const { objective, part } = center;
      // A link to the other part, once it's written: the exercise from the lesson's page, or the lesson.
      const other = part === "lesson" ? exerciseOf(ws, objective.id) : lessonOf(ws, objective.id);
      return (
        <NotWritten
          key={item && itemKey(item)}
          wsId={id}
          objective={objective}
          part={part}
          moduleTitle={moduleTitle}
          writing={!!live.busy[writerThread(objective.id)]}
          check={live.checks[objective.id] ?? null}
          failed={live.writeFailed[objective.id] ?? ""}
          other={other.kind === "todo" ? null : { label: part === "lesson" ? "Exercise →" : "← Lesson", onOpen: () => nav.open(other) }}
        />
      );
    }

    case "lesson": {
      const { lesson } = center;
      const exercise = exerciseOf(ws, lesson.objective);
      return (
        <LessonPanel
          key={lesson.id}
          wsId={id}
          lessonId={lesson.id}
          moduleTitle={moduleTitle}
          icon={ws.icon}
          lang={details.lang}
          onOpenExercise={() => nav.open(exercise)}
          exerciseWritten={exercise.kind !== "todo"}
          references={details.module?.references}
        />
      );
    }

    case "task":
      return (
        <TaskPanel
          key={center.id}
          wsId={id}
          taskId={center.id}
          passed={!!details.task?.passed}
          moduleTitle={moduleTitle}
          nextTask={details.nextTask}
          moreComing={ws.progress.passed < ws.progress.total}
          // "Next task" leads through the next objective's lesson if it isn't finished.
          onOpenTask={(taskId) => nav.open(viaLesson(ws, taskId))}
        />
      );

    case "empty":
      return <EmptyState ws={ws} writing={live.writing.size > 0} />;
  }
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
