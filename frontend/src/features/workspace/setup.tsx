import { useState } from "react";
import { ChatRole, ChatService, type Questions } from "@/lib/api";
import { PLAN_THREAD } from "@/lib/workspace";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import { QuestionsForm } from "@/features/workspace/questions";
import { AutoTextarea } from "@/components/auto-textarea";
import { ErrorText } from "@/components/error-text";
import { PageIntro, ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { usePending } from "@/hooks/use-pending";
import { WorkLog } from "@/features/workspace/work-log";

type Props = {
  questions: Questions | null;
};

/**
 * A new workspace before its first plan: the tutor's questions, then its
 * work on the mission and plan, all in the centre of the page. The plan
 * itself opens for review once proposed. There's no chat beside it yet; if
 * the tutor stops to ask something in words, the learner replies here.
 */
export function Setup({ questions }: Props) {
  const { id, live, threads } = useLiveWorkspace();
  const items = threads[PLAN_THREAD] ?? [];
  const busy = live.isBusy(PLAN_THREAD);
  // Only what happened since the learner last wrote is shown.
  const lastUser = items.findLastIndex((i) => i.role === ChatRole.RoleUser);
  const recent = items.slice(Math.max(0, lastUser));
  const answered = lastUser >= 0;

  let title = "Getting started";
  let body: string;
  if (questions) {
    body = questions.intro || "Answer a few questions so your tutor can shape your course.";
  } else if (busy) {
    if (answered) title = "Writing your plan";
    body = answered
      ? "Your tutor is writing your mission and a course plan from your answers. The plan opens here for you to review when it's ready."
      : "Your tutor is preparing a few questions about what you want to learn.";
  } else if (!items.length) {
    body = "Your tutor is getting started.";
  } else {
    title = "Your tutor is waiting for you";
    body = "Reply below to continue.";
  }

  return (
    <ReadingPage>
      <PageIntro title={title}>{body}</PageIntro>
      {questions ? (
        <QuestionsForm key={JSON.stringify(questions)} layout="page" form={questions} onAnswer={live.answerQuestions} onDismiss={live.dismissQuestions} />
      ) : (
        <>
          {(busy || recent.length > 0) && (
            <WorkLog items={recent} busy={busy} check={live.toolchainCheck} onStop={() => ChatService.StopAgent(id, PLAN_THREAD).catch(live.fail)} />
          )}
          {!busy && items.length > 0 && <Reply onSend={(text) => ChatService.SendMessage(id, text, PLAN_THREAD)} />}
        </>
      )}
    </ReadingPage>
  );
}

function Reply({ onSend }: { onSend: (text: string) => Promise<void> }) {
  const [text, setText] = useState("");
  const { pending, error, run } = usePending();
  const send = () => text.trim() && !pending && run(() => onSend(text.trim()).then(() => setText("")));
  return (
    <form
      className="mt-7 flex flex-col gap-2.5"
      onSubmit={(e) => {
        e.preventDefault();
        send();
      }}
    >
      <AutoTextarea
        maxHeight={200}
        value={text}
        placeholder="Reply to your tutor"
        aria-label="Reply to your tutor"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            send();
          }
        }}
      />
      <ErrorText error={error} />
      <div className="flex flex-wrap items-center gap-2">
        <Button disabled={!text.trim() || pending}>{pending ? "Sending…" : "Send"}</Button>
      </div>
    </form>
  );
}
