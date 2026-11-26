import { useId, useState } from "react";
import { cn } from "@/lib/utils";
import { plural } from "@/lib/format";
import type { Feedback, FeedbackChip } from "@/lib/api";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import { openPreferences } from "@/features/workspace/preferences";
import { CHIP_PAIRS, chipLabel, toggleChip, unsolvedWrittenCount } from "@/features/feedback/chips";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ErrorText } from "@/components/error-text";
import { usePending } from "@/hooks/use-pending";
import { useStored } from "@/hooks/use-stored";

type Props = {
  /** The lesson or exercise it's about, e.g. "task:0001-sum": once feedback on it is sent, the trigger confirms it instead of asking again. */
  item: string;
  /** The open lesson's id when triggered from a lesson, so regeneration keeps it on screen and excludes it from the count. */
  keepLessonId?: string;
  /** Compact toolbar mode (used in the exercise results bar) keeps the sent state on one button-sized control. */
  compact?: boolean;
  /** Sends the feedback to the planning tutor. */
  send: (fb: Feedback) => Promise<unknown>;
  className?: string;
};

/**
 * A "Submit feedback" button after a lesson or solved exercise that opens a
 * dialog with default quick picks, a detailed feedback box, and an option to
 * regenerate already-generated unsolved lessons and exercises.
 */
export function HowWasThis({ item, keepLessonId, compact, send, className }: Props) {
  const { id: wsId, live, nav } = useLiveWorkspace();
  const [sent, setSent] = useStored(`feedback-sent.${wsId}.${item}`, false);
  const [open, setOpen] = useState(false);
  const [chips, setChips] = useState<FeedbackChip[]>([]);
  const [text, setText] = useState("");
  const [regenerate, setRegenerate] = useState(false);
  const { pending, error, run } = usePending();
  const detailsId = useId();

  const known = live.ws?.known ?? "";
  const regenCount = unsolvedWrittenCount(live.ws, keepLessonId);
  const ready = chips.length > 0 || text.trim() !== "";

  function submit() {
    if (!ready || pending) return;
    run(async () => {
      await send({ chips, text: text.trim(), regenerate: regenCount > 0 && regenerate });
      setSent(true);
      setOpen(false);
    });
  }

  if (sent) {
    if (compact) {
      return (
        <Button variant="ghost" className={cn("text-muted-foreground", className)} onClick={() => openPreferences(nav)} title="See how you like to learn">
          Feedback sent
        </Button>
      );
    }
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
    <>
      <Button variant="outline" className={className} onClick={() => setOpen(true)}>
        Submit feedback
      </Button>

      <Dialog open={open} onOpenChange={(next) => !pending && setOpen(next)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Submit feedback</DialogTitle>
            <DialogDescription>Tell your tutor how this went. It updates how you like to learn for future lessons and exercises.</DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <span className="text-xs font-medium text-muted-foreground">Quick picks</span>
              <div className="flex flex-wrap gap-1.5" role="group" aria-label="Quick picks">
                {CHIP_PAIRS.flat().map((chip) => (
                  <Button
                    key={chip}
                    type="button"
                    variant="outline"
                    size="sm"
                    className="text-soft-foreground aria-pressed:border-foreground aria-pressed:bg-accent aria-pressed:text-foreground"
                    aria-pressed={chips.includes(chip)}
                    disabled={pending}
                    onClick={() => setChips((cur) => toggleChip(cur, chip))}
                  >
                    {chipLabel(chip, known)}
                  </Button>
                ))}
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor={detailsId}>
                Details <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Textarea
                id={detailsId}
                rows={4}
                placeholder="What worked, what didn't, or what would help next time?"
                value={text}
                disabled={pending}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                    e.preventDefault();
                    submit();
                  }
                }}
              />
            </div>

            {regenCount > 0 && (
              <label className="flex cursor-pointer items-start gap-2.5 rounded-lg border bg-background px-3 py-2.5 text-sm">
                <Checkbox checked={regenerate} onCheckedChange={(v) => setRegenerate(v === true)} disabled={pending} className="mt-0.5" />
                <span className="flex flex-col gap-0.5">
                  <span className="font-medium text-foreground">Regenerate unsolved lessons and exercises</span>
                  <span className="text-xs text-muted-foreground">
                    Redo {plural(regenCount, "already-generated objective")} using your updated preferences.
                  </span>
                </span>
              </label>
            )}

            <ErrorText error={error} />
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={pending}>
              Cancel
            </Button>
            <Button type="button" onClick={submit} disabled={!ready || pending}>
              {pending ? "Sending…" : "Send feedback"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
