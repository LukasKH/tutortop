import { useEffect, useState, type FormEvent } from "react";
import { ActivityPanel } from "./ActivityPanel";
import { App, errorText, type Activity, type Environment, type Summary } from "./api";
import { IDEAS, type Idea } from "./ideas";
import { Gear, Logo } from "./icons";
import { CoachLine } from "./Settings";
import { TopicIcon } from "./topicIcons";
import { ConfirmDialog, ErrorText, ProgressBar, usePending } from "./ui";

// Environment() checks the sandbox and can take a few seconds, so the last
// answer is kept while a fresh one loads.
let lastEnv: Environment | null = null;

type Props = {
  /** The coach as configured in Settings, e.g. "Claude Code · Opus · High". */
  coach: string;
  /** The workspace opened last, shown in focus. */
  lastId: string | null;
  onSettings: () => void;
  onOpen: (id: string) => void;
};

/** The topic in the new-workspace form: typed, or picked from an idea. */
type Draft = { title: string; icon: string };

export function Home({ coach, lastId, onSettings, onOpen }: Props) {
  const [env, setEnv] = useState<Environment | null>(lastEnv);
  const [workspaces, setWorkspaces] = useState<Summary[] | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<Draft>({ title: "", icon: "" });
  const [activity, setActivity] = useState<Activity | null>(null);
  const [error, setError] = useState("");

  const list = workspaces ?? [];
  // One workspace is in focus: the last one opened, else the first.
  const focusId = list.some((w) => w.id === lastId) ? lastId : list[0]?.id;

  function startFrom(idea: Idea) {
    setDraft({ title: idea.title, icon: idea.icon });
    setCreating(true);
    document.querySelector(".home-main")?.scrollTo({ top: 0, behavior: "smooth" });
  }

  useEffect(() => {
    App.ListWorkspaces()
      .then((list) => {
        setWorkspaces(list ?? []);
        setCreating((list ?? []).length === 0);
      })
      .catch((e) => setError(errorText(e)));
    App.Activity()
      .then(setActivity)
      .catch(() => {});
    App.Environment()
      .then((e) => setEnv((lastEnv = e)))
      .catch((e) => setError(errorText(e)));
  }, []);

  return (
    <div className="home">
      <header className="home-bar">
        <div className="brand">
          <Logo size={20} className="brand-mark" />
          <span className="brand-name">Tutortop</span>
        </div>
        <button type="button" className="btn settings-btn" onClick={onSettings}>
          <Gear size={16} /> Settings
        </button>
      </header>

      <main className="home-main">
        <div className="home-page">
          <header className="page-head">
            <h1>Workspaces</h1>
            {!creating && (
              <button
                className="btn ink"
                onClick={() => {
                  setDraft({ title: "", icon: "" });
                  setCreating(true);
                }}
              >
                + New workspace
              </button>
            )}
          </header>

          {env && !env.sandboxOk && (
            <p className="warning" role="alert">
              Checks need Docker or Podman, and neither is available. {env.sandboxHint && <code>{env.sandboxHint}</code>}
            </p>
          )}
          <ErrorText error={error} />

          {creating && (
            <NewWorkspace
              key={draft.title}
              draft={draft}
              env={env}
              coach={coach}
              onSettings={onSettings}
              onCancel={() => setCreating(false)}
              onCreated={onOpen}
            />
          )}

          {activity && (
            <div className={creating ? "is-dimmed" : ""}>
              <ActivityPanel activity={activity} />
            </div>
          )}

          {list.length > 0 && (
            // While a new workspace is being set up, the existing ones step back.
            <div className={`cards ${creating ? "is-dimmed" : ""}`}>
              {list.map((w) => (
                <WorkspaceCard
                  key={w.id}
                  ws={w}
                  focused={w.id === focusId && list.length > 1}
                  onOpen={() => onOpen(w.id)}
                  onDelete={async () => {
                    await App.DeleteWorkspace(w.id);
                    setWorkspaces((cur) => (cur ?? []).filter((x) => x.id !== w.id));
                  }}
                />
              ))}
            </div>
          )}

          {workspaces && (
            <section className={`ideas ${creating ? "" : "is-dimmed"}`} aria-labelledby="ideas-title">
              <header className="section-head">
                <h2 id="ideas-title">Ideas</h2>
                <p className="muted small">Start from one. Your tutor still plans it with you.</p>
              </header>
              <div className="cards">
                {IDEAS.map((idea) => (
                  <IdeaCard key={idea.title} idea={idea} picked={creating && draft.title === idea.title} onPick={() => startFrom(idea)} />
                ))}
              </div>
            </section>
          )}
        </div>
      </main>
    </div>
  );
}

/** A workspace card. Delete asks first in a popup, since it removes the code and notes for good. */
function WorkspaceCard({ ws, focused, onOpen, onDelete }: { ws: Summary; focused: boolean; onOpen: () => void; onDelete: () => Promise<void> }) {
  const [confirming, setConfirming] = useState(false);
  const { passed, total } = ws.progress;
  return (
    <div className={`card ${focused ? "is-focus" : ""}`}>
      <button className="card-open" onClick={onOpen}>
        <span className="card-head">
          <span className="card-name">{ws.title}</span>
          <TopicIcon name={ws.icon} size={30} />
        </span>
        <span className="card-text">{ws.next ? `Next: ${ws.next}` : total === 0 ? "No plan yet" : passed === total ? "All objectives passed" : ""}</span>
        <span className="card-foot">
          <ProgressBar passed={passed} total={total} />
          <span className="num">
            {passed} / {total}
          </span>
        </span>
      </button>
      <button className="btn text card-delete" onClick={() => setConfirming(true)} title="Delete this workspace">
        Delete
      </button>
      {confirming && (
        <ConfirmDialog
          title={`Delete “${ws.title}”?`}
          confirmLabel="Delete workspace"
          pendingLabel="Deleting…"
          onConfirm={onDelete}
          onCancel={() => setConfirming(false)}
        >
          <p>This removes everything in it for good: your code, mission, notes, lessons, learning records, chats and progress.</p>
          <p className="muted">
            {passed} of {total} objectives passed. This can't be undone.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

function IdeaCard({ idea, picked, onPick }: { idea: Idea; picked: boolean; onPick: () => void }) {
  return (
    <button className={`card idea ${picked ? "is-focus" : ""}`} onClick={onPick} aria-pressed={picked}>
      <span className="card-head">
        <span className="card-name">{idea.title}</span>
        <TopicIcon name={idea.icon} size={26} />
      </span>
      <span className="card-text is-wrapping">{idea.blurb}</span>
      <span className="card-foot is-row">
        <span className="num">{idea.language}</span>
        <span aria-hidden="true">→</span>
      </span>
    </button>
  );
}

function NewWorkspace({
  draft,
  env,
  coach,
  onSettings,
  onCancel,
  onCreated,
}: {
  draft: Draft;
  env: Environment | null;
  coach: string;
  onSettings: () => void;
  onCancel: () => void;
  onCreated: (id: string) => void;
}) {
  const [title, setTitle] = useState(draft.title);
  const { pending, error, setError, run } = usePending();
  const noAgent = !!env && !(env.providers ?? []).some((p) => p.installed);

  function submit(e: FormEvent) {
    e.preventDefault();
    // An idea's icon stays only while its title does; otherwise the coach picks one.
    const icon = title.trim() === draft.title ? draft.icon : "";
    if (title.trim() && !noAgent) run(async () => onCreated(await App.CreateWorkspace(title.trim(), icon)));
  }

  return (
    <form className="panel new-ws" onSubmit={submit}>
      <label className="field">
        <span className="field-label">Topic</span>
        <input
          className="input"
          value={title}
          aria-invalid={!!error}
          aria-describedby={error ? "new-ws-error" : undefined}
          onChange={(e) => {
            setTitle(e.target.value);
            setError("");
          }}
          placeholder="Go with htmx"
          autoFocus
        />
        {error && (
          <span id="new-ws-error" className="error-text" role="alert">
            {error}
          </span>
        )}
      </label>

      <div className="field">
        <span className="field-label">Tutor</span>
        <CoachLine coach={coach} onSettings={onSettings} />
        {noAgent && (
          <p className="error-text" role="alert">
            Install Claude Code or Codex, sign in, then restart Tutortop.
          </p>
        )}
      </div>

      <div className="row">
        <button className="btn ink" disabled={!title.trim() || noAgent || pending}>
          {pending ? "Creating…" : "Create"}
        </button>
        <button type="button" className="btn text" onClick={onCancel} disabled={pending}>
          Cancel
        </button>
      </div>
    </form>
  );
}
