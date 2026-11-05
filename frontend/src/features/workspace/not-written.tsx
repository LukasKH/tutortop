import { useState } from "react";
import { WorkspaceService, errorText, type Objective, type SandboxCheck } from "@/lib/api";
import type { Part } from "@/features/workspace/open-item";
import { Spinner } from "@/components/ui/spinner";
import { ErrorText } from "@/components/error-text";
import { Page, PageHeader, PageIntro, ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";

type Props = {
  wsId: string;
  objective: Pick<Objective, "id" | "name" | "title">;
  part: Part;
  moduleTitle: string;
  /** The objective's writer is at work on it (writingObjectives, workspace-state.ts). */
  writing: boolean;
  /** The writer's latest sandbox check of the exercise, if any. */
  check: SandboxCheck | null;
  /** Why the writer stopped, if it failed. */
  failed: string;
  /** The other part, if it's written: the exercise from a lesson page, or the lesson. */
  other: { label: string; onOpen: () => void } | null;
};

/**
 * An objective's lesson or exercise that isn't written yet. The learner can
 * open any objective, so this has it written now, in the background: other
 * objectives can be written at the same time, and the tutor's chat stays
 * free. The page switches to the real thing when it arrives.
 */
export function NotWritten({ wsId, objective, part, moduleTitle, writing, check, failed, other }: Props) {
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");
  // A writer that finishes while this page is still open didn't write this part.
  const [wasWriting, setWasWriting] = useState(writing);
  const [unfinished, setUnfinished] = useState(false);
  if (writing !== wasWriting) {
    setWasWriting(writing);
    setUnfinished(!writing);
  }

  const name = objective.name || objective.title;
  const what = part === "lesson" ? "lesson" : "exercise";

  async function write() {
    setError("");
    setStarting(true);
    try {
      await WorkspaceService.WriteObjectiveNow(wsId, objective.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setStarting(false);
    }
  }

  return (
    <Page>
      <PageHeader module={moduleTitle} title={name} tooltip={objective.title}>
        {other && (
          <Button variant="ghost" onClick={other.onOpen}>
            {other.label}
          </Button>
        )}
      </PageHeader>
      <ReadingPage>
        <PageIntro title={name}>{objective.name ? objective.title : null}</PageIntro>
        <div className="mt-[26px] flex flex-col gap-3.5">
          {writing || starting ? (
            <>
              <p className="flex flex-wrap items-center gap-2">
                <Spinner className="size-3.5" aria-hidden /> {status(part, check, !other)}
              </p>
              <p className="text-sm text-muted-foreground">
                It opens here when it's ready. Writing runs in the background, so you can open something else meanwhile.
              </p>
            </>
          ) : (
            <>
              <p className="text-muted-foreground">{unfinished || failed ? `The ${what} didn't get written. Try again.` : `The ${what} isn't written yet.`}</p>
              <div className="flex flex-wrap items-center gap-2">
                <Button onClick={write}>{unfinished || failed ? "Try again" : "Write it now"}</Button>
              </div>
            </>
          )}
          <ErrorText error={error || (writing ? "" : failed)} />
        </div>
      </ReadingPage>
    </Page>
  );
}

/** What the writer is doing. It writes the lesson first, then the exercise. */
function status(part: Part, check: SandboxCheck | null, otherMissing: boolean): string {
  if (check?.running) return `Checking the exercise “${check.title}” in the sandbox…`;
  if (check && !check.passed) return `Fixing the exercise “${check.title}” after the sandbox checks…`;
  if (part === "exercise" && otherMissing) return "Writing the lesson first, then the exercise…";
  return `Writing the ${part}…`;
}
