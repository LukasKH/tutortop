import { useState } from "react";
import { App, TASK_MINUTES, languageLabel, objectivesOf, plural, type Curriculum, type Progress } from "./api";
import { Markdown } from "./Markdown";
import { ErrorText, usePending } from "./ui";

type Props = {
  wsId: string;
  proposal: Curriculum;
  current: Curriculum | null;
  progress: Progress;
  /** The learner's mission, shown above a first plan so the two read together. */
  mission?: string;
};

const SHAPES: Record<string, string> = { kata: "standalone exercises", project: "one program that grows task by task" };

export function ProposalView({ wsId, proposal, current, progress, mission }: Props) {
  // The backend fills in defaults when it validates a proposal.
  const { size = "medium", shape = "kata" } = proposal.taskDefaults ?? {};
  const [editing, setEditing] = useState(false);
  const [feedback, setFeedback] = useState("");
  const { pending, error, run } = usePending();

  const objectives = progress.objectives ?? {};
  const before = new Map(objectivesOf(current).map((o) => [o.id, o.title]));
  const after = new Set(objectivesOf(proposal).map((o) => o.id));
  const removed = [...before].filter(([id]) => !after.has(id));
  const added = [...after].filter((id) => !before.has(id)).length;
  const kept = after.size - added;
  const moved = movedModules(current, proposal);
  const moduleCount = (proposal.modules ?? []).length;

  return (
    <div className="proposal">
      <div className="reading">
        <header className="empty-intro proposal-head">
          <h1>{current ? "Plan changes" : "Your plan is ready"}</h1>
          <p>
            {current
              ? "Your tutor suggests these changes. Approve them or ask for something different."
              : "Read your mission and the course plan, then approve it or ask for changes. Your first lesson and exercise follow."}
          </p>
        </header>

        {!current && mission?.trim() && <Markdown text={mission} className="prose proposal-mission" />}

        <section className="plan-curriculum">
          {!current && <h1 className="plan-title">Course plan</h1>}
          <p className="muted small proposal-facts">
            {[
              ...(current
                ? [`${added} added`, `${removed.length} removed`, `${kept} kept`, ...(moved.size ? [`${plural(moved.size, "module")} moved`] : [])]
                : [`${plural(moduleCount, "module")}`, `${plural(after.size, "objective")}`]),
              languageLabel(proposal.language),
              `${size} tasks${TASK_MINUTES[size] ? ` of about ${TASK_MINUTES[size]} min` : ""}`,
              SHAPES[shape] ?? shape,
            ].join(" · ")}
          </p>
        <ol className="plan">
          {(proposal.modules ?? []).map((m) => (
            <li key={m.id}>
              <h2 className="plan-module">
                {m.title}
                {moved.has(m.id) && (
                  <span className="muted" title="Its position in the plan changed">
                    moved
                  </span>
                )}
              </h2>
              <ul>
                {(m.objectives ?? []).map((o) => {
                  const isNew = !!current && !before.has(o.id);
                  const renamed = !!current && before.has(o.id) && before.get(o.id) !== o.title;
                  const done = objectives[o.id]?.status === "passed";
                  return (
                    <li key={o.id} className="plan-objective">
                      <span className="mark pass">{done ? "✓" : ""}</span>
                      <span className="plan-objective-text">
                        <span className="plan-objective-name">
                          <span className="strong">{o.name || o.title}</span>
                          {o.language && o.language !== proposal.language && <span className="muted">{languageLabel(o.language)}</span>}
                          {isNew && <span className="tag">new</span>}
                          {renamed && (
                            <span className="tag" title={`Was: ${before.get(o.id)}`}>
                              renamed
                            </span>
                          )}
                        </span>
                        {o.name && <span className="muted">{o.title}</span>}
                      </span>
                    </li>
                  );
                })}
              </ul>
            </li>
          ))}
        </ol>
        </section>

        {removed.length > 0 && (
          <section>
            <h2 className="plan-module">Removed</h2>
            <ul>
              {removed.map(([id, title]) => (
                <li key={id} className="plan-objective">
                  <span className="mark pass">{objectives[id]?.status === "passed" ? "✓" : ""}</span>
                  <span className="plan-objective-text">
                    <span className="struck">{title}</span>
                    {objectives[id]?.status === "passed" && <span className="muted">Still counts all-time</span>}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>

      <footer className="proposal-actions">
        <div className="proposal-actions-inner">
          <ErrorText error={error} />
          {editing ? (
            <div className="feedback">
              <textarea className="input" rows={3} autoFocus value={feedback} onChange={(e) => setFeedback(e.target.value)} placeholder="What should change?" />
              <div className="row">
                <button className="btn ink" onClick={() => run(() => App.RejectProposal(wsId, feedback.trim()))} disabled={!feedback.trim() || pending}>
                  {pending ? "Sending…" : "Send"}
                </button>
                <button className="btn text" onClick={() => setEditing(false)} disabled={pending}>
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div className="row">
              <button className="btn ink" onClick={() => run(() => App.ApproveProposal(wsId))} disabled={pending}>
                {pending ? "Approving…" : "Approve"}
              </button>
              <button className="btn" onClick={() => setEditing(true)} disabled={pending}>
                Request changes
              </button>
            </div>
          )}
        </div>
      </footer>
    </div>
  );
}

/**
 * Modules that changed position relative to the others. Modules that stay in
 * the same relative order (the longest run kept in order) aren't "moved",
 * so inserting one module doesn't flag everything after it.
 */
function movedModules(current: Curriculum | null, proposal: Curriculum): Set<string> {
  const oldIndex = new Map((current?.modules ?? []).map((m, i) => [m.id, i]));
  const common = (proposal.modules ?? []).filter((m) => oldIndex.has(m.id)).map((m) => m.id);
  // Longest increasing subsequence of old positions, O(n²) is plenty here.
  const best: number[] = common.map(() => 1);
  const prev: number[] = common.map(() => -1);
  for (let i = 0; i < common.length; i++)
    for (let j = 0; j < i; j++)
      if (oldIndex.get(common[j])! < oldIndex.get(common[i])! && best[j] + 1 > best[i]) {
        best[i] = best[j] + 1;
        prev[i] = j;
      }
  const inOrder = new Set<string>();
  for (let i = best.indexOf(Math.max(0, ...best)); i >= 0; i = prev[i]) inOrder.add(common[i]);
  return new Set(common.filter((id) => !inOrder.has(id)));
}
