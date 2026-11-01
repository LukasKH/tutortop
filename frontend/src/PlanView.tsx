import { useState } from "react";
import { App, errorText, type WorkspaceState } from "./api";
import { Pencil } from "./icons";
import { Markdown } from "./Markdown";
import { AutoTextarea, ErrorText } from "./ui";

type Props = {
  wsId: string;
  ws: WorkspaceState;
  onClose?: () => void;
  /** Opens the tutor beside the page, ready for a message about the plan. Not offered before the first plan. */
  onAskTutor?: () => void;
};

/**
 * The mission and the curriculum on one page, with the planning tutor in the
 * chat beside it. There are two ways to change them: edit the mission text
 * yourself, or ask the tutor, who proposes curriculum changes for you to
 * review. The curriculum isn't edited by hand.
 */
export function PlanView({ wsId, ws, onClose, onAskTutor }: Props) {
  const [editing, setEditing] = useState(false);
  if (editing) return <MissionEditor wsId={wsId} mission={ws.mission} onDone={() => setEditing(false)} />;

  const objectives = ws.progress.objectives ?? {};
  const passed = (id: string) => objectives[id]?.status === "passed";
  const modules = ws.curriculum?.modules ?? [];
  return (
    <div className="reading plan-page">
      <div className="plan-bar">
        {onClose ? (
          <button className="btn text back-link" onClick={onClose}>
            ← Back
          </button>
        ) : (
          <span />
        )}
        <div className="row">
          <button className="btn" onClick={() => setEditing(true)}>
            <Pencil size={14} /> Edit mission
          </button>
          {onAskTutor && (
            <button className="btn" onClick={onAskTutor}>
              Change with the tutor
            </button>
          )}
        </div>
      </div>
      <p className="plan-how muted small">
        {onAskTutor
          ? "Edit your mission yourself, or ask the tutor to change it or the curriculum. You review the tutor's changes before they apply."
          : "Edit your mission yourself. Your tutor builds the course plan from it."}
      </p>
      {ws.mission.trim() ? (
        <Markdown text={ws.mission} className="prose" />
      ) : (
        <div className="prose">
          <h1>Mission</h1>
          <p className="muted">The tutor writes your mission after the first questions.</p>
        </div>
      )}
      {ws.curriculum && (
        <section className="plan-curriculum">
          <h1 className="plan-title">Curriculum</h1>
          <ol className="plan">
            {modules.map((m) => {
              const objs = m.objectives ?? [];
              return (
                <li key={m.id}>
                  <h2 className="plan-module">
                    {m.title}{" "}
                    <span className="muted num">
                      {objs.filter((o) => passed(o.id)).length}/{objs.length}
                    </span>
                  </h2>
                  <ul>
                    {objs.map((o) => (
                      <li key={o.id} className="plan-objective">
                        <span className="mark pass">{passed(o.id) ? "✓" : ""}</span>
                        <span className="plan-objective-text">
                          <span className="strong">{o.name || o.title}</span>
                          {o.name && <span className="muted">{o.title}</span>}
                        </span>
                      </li>
                    ))}
                  </ul>
                </li>
              );
            })}
          </ol>
        </section>
      )}
    </div>
  );
}

/** The mission as plain text. Saving tells the tutor, who proposes plan changes to match if needed. */
function MissionEditor({ wsId, mission: initial, onDone }: { wsId: string; mission: string; onDone: () => void }) {
  const [mission, setMission] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function save() {
    setSaving(true);
    setError("");
    try {
      await App.SaveMission(wsId, mission);
      onDone();
    } catch (e) {
      setError(errorText(e));
      setSaving(false);
    }
  }

  return (
    <div className="reading plan-page">
      <div className="plan-bar">
        <span className="muted small">Your tutor reads the new mission and suggests plan changes to match, if it needs any.</span>
        <div className="row">
          <button className="btn text" onClick={onDone} disabled={saving}>
            Cancel
          </button>
          <button className="btn ink" onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
      <ErrorText error={error} className="plan-error" />
      <label className="field">
        <span className="plan-title">Mission</span>
        <AutoTextarea className="input mission-input" value={mission} maxHeight={640} onChange={(e) => setMission(e.target.value)} autoFocus />
      </label>
    </div>
  );
}
