import { useState } from "react";
import { type ChatItem, type GateEvent, type QuestionForm } from "./api";
import { QuestionsForm } from "./Questions";
import { AutoTextarea, ErrorText, usePending } from "./ui";
import { WorkLog } from "./WorkLog";

type Props = {
  /** The planning conversation so far. */
  items: ChatItem[];
  busy: boolean;
  gate: GateEvent | null;
  questions: QuestionForm | null;
  lang?: string;
  onAnswer: (answers: Record<string, string>) => Promise<void>;
  onDismiss: () => void;
  onStop: () => void;
  onSend: (text: string) => Promise<void>;
};

/**
 * A new workspace before its first plan: the tutor's questions, then its
 * work on the mission and plan, all in the centre of the page. The plan
 * itself opens for review once proposed. There's no chat beside it yet; if
 * the tutor stops to ask something in words, the learner replies here.
 */
export function Setup({ items, busy, gate, questions, lang, onAnswer, onDismiss, onStop, onSend }: Props) {
  // Only what happened since the learner last wrote is shown.
  const lastUser = items.findLastIndex((i) => i.role === "user");
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
    <div className="reading setup">
      <div className="empty-intro">
        <h1>{title}</h1>
        <p>{body}</p>
      </div>
      {questions ? (
        <QuestionsForm key={JSON.stringify(questions)} layout="page" form={questions} onAnswer={onAnswer} onDismiss={onDismiss} />
      ) : (
        <>
          {(busy || recent.length > 0) && <WorkLog items={recent} busy={busy} gate={gate} lang={lang} onStop={onStop} />}
          {!busy && items.length > 0 && <Reply onSend={onSend} />}
        </>
      )}
    </div>
  );
}

function Reply({ onSend }: { onSend: (text: string) => Promise<void> }) {
  const [text, setText] = useState("");
  const { pending, error, run } = usePending();
  const send = () => text.trim() && !pending && run(() => onSend(text.trim()).then(() => setText("")));
  return (
    <form
      className="setup-reply"
      onSubmit={(e) => {
        e.preventDefault();
        send();
      }}
    >
      <AutoTextarea
        className="input"
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
      <ErrorText error={error} className="small" />
      <div className="row">
        <button className="btn ink" disabled={!text.trim() || pending}>
          {pending ? "Sending…" : "Send"}
        </button>
      </div>
    </form>
  );
}
