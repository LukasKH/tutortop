import { useId, useState } from "react";
import { type Question, type QuestionForm } from "./api";
import { AutoTextarea, ErrorText, usePending } from "./ui";

type Props = {
  form: QuestionForm;
  /** "page" lays the form out in the centre pane; "chat" fits the chat column. */
  layout: "page" | "chat";
  onAnswer: (answers: Record<string, string>) => Promise<void>;
  onDismiss: () => void;
};

/** The tutor's ask_user form: suggested options plus a free answer for every question. */
export function QuestionsForm({ form, layout, onAnswer, onDismiss }: Props) {
  const formId = useId();
  const questions = form.questions ?? [];
  const [picked, setPicked] = useState<Record<string, string[]>>({});
  const [other, setOther] = useState<Record<string, string>>({});
  const { pending, error, run } = usePending();

  const answerFor = (q: Question) => {
    const own = (other[q.id] ?? "").trim();
    return [...(picked[q.id] ?? []), ...(own ? [own] : [])].join("; ");
  };
  const complete = questions.every((q) => answerFor(q) !== "");

  function choose(q: Question, option: string, on: boolean) {
    setPicked((cur) => {
      const sel = cur[q.id] ?? [];
      if (!q.multiSelect) return { ...cur, [q.id]: on ? [option] : [] };
      return { ...cur, [q.id]: on ? [...sel, option] : sel.filter((x) => x !== option) };
    });
    // In a single-choice question, a suggestion and a written answer replace each other.
    if (!q.multiSelect && on) setOther((cur) => ({ ...cur, [q.id]: "" }));
  }

  return (
    <form
      className={`questions questions-${layout}`}
      onSubmit={(e) => {
        e.preventDefault();
        if (complete && !pending) run(() => onAnswer(Object.fromEntries(questions.map((q) => [q.id, answerFor(q)]))));
      }}
    >
      {layout === "chat" && form.intro && <p className="questions-intro">{form.intro}</p>}
      {questions.map((q, qi) => {
        const options = q.options ?? [];
        return (
          <fieldset key={q.id} className="question">
            <legend>
              {layout === "page" && <span className="question-num">{qi + 1}</span>}
              {q.question}
            </legend>
            {options.length > 0 && (
              <div className="choices">
                {options.map((o) => {
                  const on = (picked[q.id] ?? []).includes(o);
                  return (
                    <label key={o} className="choice">
                      <input
                        type={q.multiSelect ? "checkbox" : "radio"}
                        name={`${formId}-${q.id}`}
                        checked={on}
                        onChange={(e) => choose(q, o, e.target.checked)}
                        // Radios can't be unticked natively; a second click does it here.
                        onClick={() => on && !q.multiSelect && choose(q, o, false)}
                      />
                      <span className={`choice-mark ${q.multiSelect ? "is-box" : "is-round"}`} aria-hidden="true" />
                      <span>{o}</span>
                    </label>
                  );
                })}
              </div>
            )}
            <AutoTextarea
              className="input question-other"
              maxHeight={160}
              value={other[q.id] ?? ""}
              aria-label={options.length ? `${q.question} (your own answer)` : q.question}
              placeholder={options.length ? "Other" : "Answer"}
              onChange={(e) => {
                const v = e.target.value;
                setOther((cur) => ({ ...cur, [q.id]: v }));
                if (!q.multiSelect && v.trim()) setPicked((cur) => ({ ...cur, [q.id]: [] }));
              }}
            />
            {q.multiSelect && <span className="muted small">Pick any</span>}
          </fieldset>
        );
      })}
      <ErrorText error={error} className="small" />
      <div className="row">
        <button className="btn ink" disabled={!complete || pending}>
          {pending ? "Sending…" : "Send"}
        </button>
        <button type="button" className="btn text" onClick={onDismiss} disabled={pending}>
          Dismiss
        </button>
        {!complete && <span className="muted small">Answer each question to send.</span>}
      </div>
    </form>
  );
}
