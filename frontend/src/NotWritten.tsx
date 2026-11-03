import { useEffect, useRef, useState } from "react";
import { App, errorText, type Gate, type Part } from "./api";
import { Spinner } from "./icons";
import { ErrorText } from "./ui";

type Props = {
  wsId: string;
  objective: { id: string; name?: string; title: string };
  part: Part;
  moduleTitle: string;
  /** The objective's writer is at work on it. */
  writing: boolean;
  /** The writer's latest sandbox check of the exercise, if any. */
  check: Gate | null;
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
  const wasWriting = useRef(writing);
  const [unfinished, setUnfinished] = useState(false);
  useEffect(() => {
    if (wasWriting.current && !writing) setUnfinished(true);
    if (writing) setUnfinished(false);
    wasWriting.current = writing;
  }, [writing]);

  const name = objective.name || objective.title;
  const what = part === "lesson" ? "lesson" : "exercise";

  async function write() {
    setError("");
    setStarting(true);
    try {
      await App.PrepareObjective(wsId, objective.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setStarting(false);
    }
  }

  return (
    <div className="task">
      <header className="taskbar">
        <div className="crumbs truncate" title={objective.title}>
          {moduleTitle && <span className="muted">{moduleTitle} / </span>}
          <b>{name}</b>
        </div>
        {other && (
          <button className="btn text" onClick={other.onOpen}>
            {other.label}
          </button>
        )}
      </header>
      <div className="reading">
        <div className="empty-intro">
          <h1>{name}</h1>
          {objective.name && <p>{objective.title}</p>}
        </div>
        <div className="not-written">
          {writing || starting ? (
            <>
              <p className="row">
                <Spinner /> {status(part, check, !other)}
              </p>
              <p className="muted small">It opens here when it's ready. Writing runs in the background, so you can open something else meanwhile.</p>
            </>
          ) : (
            <>
              <p className="muted">{unfinished || failed ? `The ${what} didn't get written. Try again.` : `The ${what} isn't written yet.`}</p>
              <div className="row">
                <button className="btn ink" onClick={write}>
                  {unfinished || failed ? "Try again" : "Write it now"}
                </button>
              </div>
            </>
          )}
          <ErrorText error={error || (writing ? "" : failed)} />
        </div>
      </div>
    </div>
  );
}

/** What the writer is doing. It writes the lesson first, then the exercise. */
function status(part: Part, check: Gate | null, otherMissing: boolean): string {
  if (check?.running) return `Checking the exercise “${check.title}” in the sandbox…`;
  if (check && !check.passed) return `Fixing the exercise “${check.title}” after the sandbox checks…`;
  if (part === "exercise" && otherMissing) return "Writing the lesson first, then the exercise…";
  return `Writing the ${part}…`;
}
