import { useState, type KeyboardEvent } from "react";
import { cn } from "@/lib/utils";
import { type Question, type Questions } from "@/lib/api";
import { AutoTextarea } from "@/components/auto-textarea";
import { ErrorText } from "@/components/error-text";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { usePending } from "@/hooks/use-pending";

type Props = {
  form: Questions;
  /** "page" lays the form out in the centre pane; "chat" fits the chat column. */
  layout: "page" | "chat";
  onAnswer: (answers: Record<string, string>) => Promise<void>;
  onDismiss: () => void;
};

/** The tutor's ask_user form: suggested options plus a free answer for every question. */
export function QuestionsForm({ form, layout, onAnswer, onDismiss }: Props) {
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
      className={cn("flex flex-col gap-[18px]", layout === "chat" ? "rounded-xl border p-3.5" : "mt-[26px] gap-[26px]")}
      onSubmit={(e) => {
        e.preventDefault();
        if (complete && !pending) run(() => onAnswer(Object.fromEntries(questions.map((q) => [q.id, answerFor(q)]))));
      }}
    >
      {layout === "chat" && form.intro && <p className="text-soft-foreground">{form.intro}</p>}
      {questions.map((q, qi) => {
        const options = q.options ?? [];
        return (
          <fieldset key={q.id} className="flex min-w-0 flex-col gap-1.5">
            <legend className={cn("mb-1.5 flex gap-2 font-medium text-foreground", layout === "page" && "text-lg")}>
              {layout === "page" && <span className="font-mono font-normal text-muted-foreground">{qi + 1}</span>}
              {q.question}
            </legend>
            {options.length > 0 && <Choices question={q} picked={picked[q.id] ?? []} onChoose={(o, on) => choose(q, o, on)} />}
            <AutoTextarea
              className="mt-0.5 overflow-x-hidden"
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
            {q.multiSelect && <span className="text-sm text-muted-foreground">Pick any</span>}
          </fieldset>
        );
      })}
      <ErrorText error={error} />
      <div className="flex flex-wrap items-center gap-2">
        <Button disabled={!complete || pending}>{pending ? "Sending…" : "Send"}</Button>
        <Button type="button" variant="ghost" onClick={onDismiss} disabled={pending}>
          Dismiss
        </Button>
        {!complete && <span className="text-sm text-muted-foreground">Answer each question to send.</span>}
      </div>
    </form>
  );
}

/** A row per suggestion: grey text that brightens when picked. */
const CHOICE =
  "relative cursor-pointer gap-2.5 py-[5px] text-[length:inherit] leading-[inherit] font-normal text-soft-foreground has-[[data-state=checked]]:text-foreground";

/** A question's suggestions: pick one, or any in a multi-select. */
function Choices({ question: q, picked, onChoose }: { question: Question; picked: string[]; onChoose: (option: string, on: boolean) => void }) {
  const options = q.options ?? [];
  if (q.multiSelect)
    return (
      <div className="flex flex-col">
        {options.map((o) => (
          <Label key={o} className={CHOICE}>
            <Checkbox checked={picked.includes(o)} onCheckedChange={(on) => onChoose(o, on === true)} onKeyDown={submitOnEnter} />
            <span>{o}</span>
          </Label>
        ))}
      </div>
    );
  return (
    <RadioGroup className="flex flex-col gap-0" value={picked[0] ?? ""} onValueChange={(o) => onChoose(o, true)}>
      {options.map((o) => (
        <Label key={o} className={CHOICE}>
          <RadioGroupItem
            value={o}
            // A picked radio doesn't untick on click; a second click does it here.
            onClick={() => picked.includes(o) && onChoose(o, false)}
            onKeyDown={submitOnEnter}
          />
          <span>{o}</span>
        </Label>
      ))}
    </RadioGroup>
  );
}

/** Radix radios and checkboxes ignore Enter; in a form it should send the answers, as it does from a text field. */
function submitOnEnter(e: KeyboardEvent<HTMLButtonElement>) {
  if (e.key === "Enter") e.currentTarget.form?.requestSubmit();
}
