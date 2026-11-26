import { useState } from "react";
import { cn } from "@/lib/utils";
import type { Feedback, FeedbackChip } from "@/lib/api";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import { openPreferences } from "@/features/workspace/preferences";
import { CHIP_PAIRS, chipLabel, toggleChip } from "@/features/feedback/chips";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ErrorText } from "@/components/error-text";
import { usePending } from "@/hooks/use-pending";
import { useStored } from "@/hooks/use-stored";

type Props = {
  /** The lesson or exercise it's about, e.g. "task:0001-sum": once feedback on it is sent, the line says thanks instead of asking again. */
  item: string;
  /** Sends the feedback to the planning tutor. */
  send: (fb: Feedback) => Promise<unknown>;
  className?: string;
};

/**
 * "How was this?" after a lesson or a solved exercise: chips to tap and an
 * optional line of the learner's own words. It never blocks the way on;
 * ignoring it sends nothing.
 */
export function HowWasThis({ item, send, className }: Props) {
  const { id: wsId, live, nav } = useLiveWorkspace();
  const [sent, setSent] = useStored(`feedback-sent.${wsId}.${item}`, false);
  const [chips, setChips] = useState<FeedbackChip[]>([]);
  // null until "Say more…" opens the text field.
  const [text, setText] = useState<string | null>(null);
  const { pending, error, run } = usePending();
  const known = live.ws?.known ?? "";
  const ready = chips.length > 0 || !!text?.trim();

  function submit() {
    if (!ready) return;
    run(async () => {
      await send({ chips, text: text?.trim() ?? "" });
      setSent(true);
    });
  }

  if (sent) {
    return (
      <p className={cn("flex flex-wrap items-center gap-x-1.5 text-sm text-muted-foreground", className)} role="status">
        Thanks, your tutor will adjust future lessons.
        <Button variant="link" size="sm" className="h-auto px-0" onClick={() => openPreferences(nav)}>
          See how you like to learn →
        </Button>
      </p>
    );
  }

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-sm" role="group" aria-label="How was this?">
        <span className="text-muted-foreground">How was this?</span>
        {CHIP_PAIRS.map((pair) => (
          <span key={pair[0]} className="flex gap-1">
            {pair.map((chip) => (
              <Button
                key={chip}
                variant="outline"
                size="sm"
                className="text-soft-foreground aria-pressed:border-foreground aria-pressed:text-foreground"
                aria-pressed={chips.includes(chip)}
                disabled={pending}
                onClick={() => setChips((cur) => toggleChip(cur, chip))}
              >
                {chipLabel(chip, known)}
              </Button>
            ))}
          </span>
        ))}
        {text === null ? (
          <Button variant="ghost" size="sm" onClick={() => setText("")} disabled={pending}>
            Say more…
          </Button>
        ) : (
          <Input
            className="h-7 w-64"
            aria-label="Say more"
            placeholder="What would help?"
            value={text}
            disabled={pending}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
            autoFocus
          />
        )}
        {ready && (
          <Button variant="outline" size="sm" onClick={submit} disabled={pending}>
            {pending ? "Sending…" : "Send"}
          </Button>
        )}
      </div>
      <ErrorText error={error} />
    </div>
  );
}
