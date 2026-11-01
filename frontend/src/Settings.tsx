import { useEffect, useRef, useState } from "react";
import { App, errorText, providerName, type AgentModel, type Environment, type Settings } from "./api";
import { ErrorText } from "./ui";

const EFFORT_LABELS: Record<string, string> = {
  none: "None",
  minimal: "Minimal",
  low: "Low",
  medium: "Medium",
  high: "High",
  xhigh: "Extra high",
  max: "Max",
  ultra: "Ultra",
};

export function effortLabel(id: string) {
  return EFFORT_LABELS[id] ?? id.charAt(0).toUpperCase() + id.slice(1);
}

// Model lists per agent, kept for the session: Codex is asked live, which takes a moment.
const modelCache = new Map<string, AgentModel[]>();

export function loadModels(agent: string): Promise<AgentModel[]> {
  const cached = modelCache.get(agent);
  if (cached) return Promise.resolve(cached);
  return App.Models(agent).then((list) => {
    modelCache.set(agent, list ?? []);
    return list ?? [];
  });
}

/**
 * The chosen model. No choice means the agent's default: Claude Code lists
 * that as its own entry; for Codex it's the default model, listed first.
 */
function currentModel(models: AgentModel[] | null, id: string): AgentModel | null {
  if (!models) return null;
  return models.find((m) => m.id === id) ?? (id === "" ? (models[0] ?? null) : null);
}

/** "Claude Code · Opus · High": the coach as configured. */
export function coachLabel(s: Settings): string {
  const choice = s.coach?.[s.agent] ?? { model: "", effort: "" };
  const model = currentModel(modelCache.get(s.agent) ?? null, choice.model);
  const parts = [providerName(s.agent)];
  if (model?.id || choice.model) parts.push(model?.name ?? choice.model);
  const effort = choice.effort || model?.defaultEffort;
  if (effort) parts.push(effortLabel(effort));
  return parts.join(" · ");
}

/** The configured coach with a way to change it. */
export function CoachLine({ coach, action = "Change", onSettings }: { coach: string; action?: string; onSettings: () => void }) {
  return (
    <div className="coach-line">
      <span className="truncate" title={coach}>
        {coach || "…"}
      </span>
      <button type="button" className="btn text" onClick={onSettings}>
        {action}
      </button>
    </div>
  );
}

type Props = {
  settings: Settings;
  onChange: (s: Settings) => Promise<void>;
  onClose: () => void;
};

/** App settings in a modal: which agent coaches, with which model and reasoning effort. */
export function SettingsDialog({ settings, onChange, onClose }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const [env, setEnv] = useState<Environment | null>(null);
  const [models, setModels] = useState<AgentModel[] | null>(modelCache.get(settings.agent) ?? null);
  const [modelsError, setModelsError] = useState("");
  const [error, setError] = useState("");

  const agent = settings.agent;
  const choice = settings.coach?.[agent] ?? { model: "", effort: "" };

  useEffect(() => {
    ref.current?.showModal();
    App.Environment()
      .then(setEnv)
      .catch(() => {});
  }, []);

  useEffect(() => {
    let live = true;
    setModels(modelCache.get(agent) ?? null);
    setModelsError("");
    loadModels(agent)
      .then((list) => live && setModels(list))
      .catch((e) => live && setModelsError(errorText(e)));
    return () => {
      live = false;
    };
  }, [agent]);

  function save(next: Settings) {
    setError("");
    onChange(next).catch((e) => setError(errorText(e)));
  }

  function choose(model: string, effort: string) {
    save({ ...settings, coach: { ...settings.coach, [agent]: { model, effort } } });
  }

  const selected = currentModel(models, choice.model);
  // The chosen model is gone from the list (e.g. retired): still show it, so it can be changed.
  const shownModels = models && choice.model && selected?.id !== choice.model ? [...models, { id: choice.model, name: choice.model, description: "", efforts: [] }] : models;
  // Without a model default (Claude Code's own default model), "Default" lets the CLI decide.
  const efforts = selected?.efforts?.length && !selected.defaultEffort ? [{ id: "", description: "Let the agent decide" }, ...selected.efforts] : (selected?.efforts ?? []);
  const effort = choice.effort || selected?.defaultEffort || "";

  return (
    // Closing unmounts the dialog, which also ends its modal state. Escape and a
    // click on the backdrop close it too.
    <dialog
      ref={ref}
      className="settings"
      aria-labelledby="settings-title"
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => e.target === e.currentTarget && onClose()}
      // Escape is handled here too: the dialog's own cancel event doesn't fire in every webview.
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
        }
      }}
    >
      <header className="settings-head">
        <h1 id="settings-title">Settings</h1>
        <button className="btn text" onClick={onClose}>
          Close
        </button>
      </header>

      <section className="settings-body">
        <h2 className="settings-section">Tutor</h2>

        <fieldset className="field">
          <legend className="field-label">Agent</legend>
          {(env?.providers ?? [{ id: "claude", name: "Claude Code", installed: true }, { id: "codex", name: "Codex", installed: true }]).map((p) => (
            <label key={p.id} className="choice" title={p.path || undefined}>
              <input
                type="radio"
                name="agent"
                checked={agent === p.id}
                disabled={!p.installed}
                onChange={() => save({ ...settings, agent: p.id })}
              />
              <span className="choice-mark is-round" aria-hidden="true" />
              <span>{p.name}</span>
              {!p.installed && <span className="muted">not installed</span>}
            </label>
          ))}
        </fieldset>

        <fieldset className="field">
          <legend className="field-label">Model</legend>
          {modelsError ? (
            <p className="error-text">{modelsError}</p>
          ) : !shownModels ? (
            <p className="muted">Loading {providerName(agent)} models…</p>
          ) : (
            shownModels.map((m) => (
              <label key={m.id || "default"} className="choice">
                <input
                  type="radio"
                  name="model"
                  checked={selected?.id === m.id}
                  // Keep the effort if the new model offers it.
                  onChange={() => choose(m.id, (m.efforts ?? []).some((e) => e.id === choice.effort) ? choice.effort : "")}
                />
                <span className="choice-mark is-round" aria-hidden="true" />
                <span>{m.name}</span>
                {m.description && <span className="muted truncate">{m.description}</span>}
              </label>
            ))
          )}
        </fieldset>

        {efforts.length > 0 && (
          <fieldset className="field">
            <legend className="field-label">Reasoning</legend>
            <div className="choices is-row">
              {efforts.map((e) => (
                <label key={e.id || "default"} className="choice" title={e.description}>
                  <input type="radio" name="effort" checked={effort === e.id} onChange={() => choose(choice.model, e.id)} />
                  <span className="choice-mark is-round" aria-hidden="true" />
                  <span>{e.id ? effortLabel(e.id) : "Default"}</span>
                  {e.id === selected?.defaultEffort && <span className="muted">default</span>}
                </label>
              ))}
            </div>
            <p className="field-note">More reasoning gives better plans and tasks, but replies take longer and use more of your plan's quota.</p>
          </fieldset>
        )}

        <p className="field-note">
          Changes apply from the tutor's next message. Switching agent starts new conversations; your mission, notes and progress stay.
        </p>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
