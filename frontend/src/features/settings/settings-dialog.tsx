import { useEffect, useState, type ReactNode } from "react";
import { cn } from "cn";
import { Dialogs } from "@wailsio/runtime";
import { SettingsService, errorText, type Model, type Environment, type Settings } from "@/lib/api";
import { cachedModels, cachedProviders, currentModel, effortLabel, loadModels, providerName } from "@/lib/agents";
import { ErrorText } from "@/components/error-text";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";

type Props = {
  settings: Settings;
  onChange: (s: Settings) => Promise<void>;
  onClose: () => void;
};

/** App settings in a modal: which agent tutors, with which model and reasoning effort. */
export function SettingsDialog({ settings, onChange, onClose }: Props) {
  const [env, setEnv] = useState<Environment | null>(null);
  // The models as last loaded, for the agent they were loaded for.
  const [loaded, setLoaded] = useState<{ agent: string; models: Model[] | null; error: string } | null>(null);
  const [error, setError] = useState("");

  const agent = settings.agent;
  const choice = settings.models?.[agent] ?? { model: "", effort: "" };

  useEffect(() => {
    SettingsService.Environment()
      .then(setEnv)
      .catch(() => {});
  }, []);

  useEffect(() => {
    let live = true;
    loadModels(agent)
      .then((list) => live && setLoaded({ agent, models: list, error: "" }))
      .catch((e) => live && setLoaded({ agent, models: null, error: errorText(e) }));
    return () => {
      live = false;
    };
  }, [agent]);
  const current = loaded?.agent === agent ? loaded : null;
  const models = current?.models ?? cachedModels(agent);
  const modelsError = current?.error ?? "";

  function save(next: Settings) {
    setError("");
    onChange(next).catch((e) => setError(errorText(e)));
  }

  function choose(model: string, effort: string) {
    save({ ...settings, models: { ...settings.models, [agent]: { model, effort } } });
  }

  const selected = currentModel(models, choice.model);
  // The chosen model is gone from the list (e.g. retired): still show it, so it can be changed.
  const shownModels =
    models && choice.model && selected?.id !== choice.model ? [...models, { id: choice.model, name: choice.model, description: "", efforts: [] }] : models;
  // Without a model default (Claude Code's own default model), "Default" lets the CLI decide.
  const efforts =
    selected?.efforts?.length && !selected.defaultEffort ? [{ id: "", description: "Let the agent decide" }, ...selected.efforts] : (selected?.efforts ?? []);
  const effort = choice.effort || selected?.defaultEffort || "";
  // Until a fresh check of what's installed arrives, the agents as the app last saw them, or at least the chosen one.
  const providers = env?.providers ?? cachedProviders() ?? [{ id: agent, name: providerName(agent), installed: true }];

  return (
    // Closing unmounts the dialog. Escape and a click on the backdrop close it too.
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        showCloseButton={false}
        aria-describedby={undefined}
        className="block max-h-[calc(100vh-96px)] w-[min(520px,calc(100vw-48px))] max-w-none overflow-auto p-0 text-base sm:max-w-none"
      >
        <header className="sticky top-0 flex items-center justify-between border-b bg-card px-5 pt-4 pb-3">
          <DialogTitle asChild>
            <h1 className="text-[20px] leading-[1.2] font-bold">Settings</h1>
          </DialogTitle>
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </header>

        <section className="flex flex-col gap-[18px] px-5 pt-4 pb-5">
          <h2 className={SECTION}>Tutor</h2>

          <fieldset className={FIELD}>
            <legend className={FIELD_LABEL}>Agent</legend>
            <RadioGroup aria-label="Agent" className={CHOICES} value={agent} onValueChange={(id) => save({ ...settings, agent: id })}>
              {providers.map((p) => (
                <Choice key={p.id} value={p.id} title={p.path || undefined} disabled={!p.installed}>
                  <span>{p.name}</span>
                  {!p.installed && <span className={CHOICE_NOTE}>not installed</span>}
                </Choice>
              ))}
            </RadioGroup>
          </fieldset>

          <fieldset className={FIELD}>
            <legend className={FIELD_LABEL}>Model</legend>
            {modelsError ? (
              <ErrorText error={modelsError} />
            ) : !shownModels ? (
              <p className="text-muted-foreground">Loading {providerName(agent)} models…</p>
            ) : (
              <RadioGroup
                aria-label="Model"
                className={CHOICES}
                value={selected?.id ?? null}
                onValueChange={(id) => {
                  const m = shownModels.find((m) => m.id === id);
                  // Keep the effort if the new model offers it.
                  if (m) choose(m.id, (m.efforts ?? []).some((e) => e.id === choice.effort) ? choice.effort : "");
                }}
              >
                {shownModels.map((m) => (
                  <Choice key={m.id || "default"} value={m.id}>
                    <span>{m.name}</span>
                    {m.description && <span className={cn(CHOICE_NOTE, "min-w-0 truncate")}>{m.description}</span>}
                  </Choice>
                ))}
              </RadioGroup>
            )}
          </fieldset>

          {efforts.length > 0 && (
            <fieldset className={FIELD}>
              <legend className={FIELD_LABEL}>Reasoning</legend>
              <RadioGroup
                aria-label="Reasoning"
                className="flex flex-row flex-wrap gap-x-[18px] gap-y-0"
                value={effort}
                onValueChange={(id) => choose(choice.model, id)}
              >
                {efforts.map((e) => (
                  <Choice key={e.id || "default"} value={e.id} title={e.description}>
                    <span>{e.id ? effortLabel(e.id) : "Default"}</span>
                    {e.id === selected?.defaultEffort && <span className={CHOICE_NOTE}>default</span>}
                  </Choice>
                ))}
              </RadioGroup>
              <p className={FIELD_NOTE}>More reasoning gives better plans and tasks, but replies take longer and use more of your plan's quota.</p>
            </fieldset>
          )}

          <p className={FIELD_NOTE}>
            Changes apply from the tutor's next message. Switching agent starts new conversations; your mission, notes and progress stay.
          </p>

          <h2 className={SECTION}>Files</h2>
          <WorkspacesFolder
            env={env}
            folder={settings.workspacesFolder ?? ""}
            onChange={(f) => save({ ...settings, workspacesFolder: f })}
            onError={setError}
          />

          <ErrorText error={error} />
        </section>
      </DialogContent>
    </Dialog>
  );
}

/** The configured tutor with a way to change it. */
export function TutorLine({ tutor, action = "Change", onSettings }: { tutor: string; action?: string; onSettings: () => void }) {
  return (
    <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
      <span className="min-w-0 truncate" title={tutor}>
        {tutor || "…"}
      </span>
      <Button type="button" variant="ghost" className="flex-none" onClick={onSettings}>
        {action}
      </Button>
    </div>
  );
}

/** A radio row: the mark, then its label and any grey note. Disabled rows step back. */
function Choice({ value, title, disabled, children }: { value: string; title?: string; disabled?: boolean; children: ReactNode }) {
  return (
    <Label
      title={title}
      className="cursor-pointer gap-2.5 py-[5px] text-base leading-normal font-normal text-soft-foreground has-disabled:cursor-default has-disabled:opacity-55 has-[[data-state=checked]]:text-foreground"
    >
      <RadioGroupItem value={value} disabled={disabled} className="disabled:cursor-default disabled:opacity-100" />
      {children}
    </Label>
  );
}

/**
 * Where workspaces live. The app opens workspaces by name while it runs, so a
 * new folder is used from the next start.
 */
function WorkspacesFolder({
  env,
  folder,
  onChange,
  onError,
}: {
  env: Environment | null;
  folder: string;
  onChange: (folder: string) => void;
  onError: (error: string) => void;
}) {
  if (!env) return <p className="text-muted-foreground">Loading…</p>;
  const shown = env.rootFromEnv ? env.root : folder || env.defaultRoot;
  const pending = !env.rootFromEnv && shown !== env.root;

  function pick() {
    // The system's own folder picker. Not available in server mode (a browser).
    Dialogs.OpenFile({
      Title: "Choose where to keep workspaces",
      ButtonText: "Use this folder",
      Directory: shown,
      CanChooseDirectories: true,
      CanChooseFiles: false,
      CanCreateDirectories: true,
    })
      .then((path) => path && onChange(path))
      .catch((e) => onError(errorText(e)));
  }

  return (
    <div className={FIELD}>
      <span className={FIELD_LABEL}>Workspaces folder</span>
      <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
        <code className="min-w-0 truncate" title={shown}>
          {shown}
        </code>
        {!env.rootFromEnv && (
          <span className="flex flex-none gap-1">
            {folder && (
              <Button type="button" variant="ghost" onClick={() => onChange("")}>
                Use default
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={pick}>
              Change…
            </Button>
          </span>
        )}
      </div>
      <p className={FIELD_NOTE}>
        {env.rootFromEnv ? (
          <>Set by the TUTORTOP_HOME environment variable, which overrides this setting.</>
        ) : pending ? (
          <>
            Tutortop switches to this folder the next time it starts. Workspaces in <code>{env.root}</code> stay where they are; move their folders over to keep
            them.
          </>
        ) : (
          <>
            Each workspace is a folder of plain files with its own git history. Hidden tests and reference solutions are kept elsewhere, so the tutor can't see
            them.
          </>
        )}
      </p>
    </div>
  );
}

const SECTION = "text-sm font-semibold text-foreground";

const FIELD = "flex min-w-0 flex-col gap-1.5";

const FIELD_LABEL = "text-sm text-muted-foreground";

const FIELD_NOTE = "text-xs text-muted-foreground";

const CHOICES = "flex flex-col gap-1.5";

const CHOICE_NOTE = "text-sm text-muted-foreground";
