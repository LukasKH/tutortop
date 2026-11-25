import { useEffect, useState, type ReactNode } from "react";
import { Check, RefreshCw } from "lucide-react";
import { RadioGroup as RadioGroupPrimitive, Tabs as TabsPrimitive } from "radix-ui";
import { errorText, type Model, type Prefs, type ProviderStatus } from "@/lib/api";
import { agentOn, checkEnvironment, currentModel, effortLabel, loadModels, useCheckingEnvironment, useEnvironment, useModels } from "@/lib/agents";
import { cn } from "@/lib/utils";
import { useSettings } from "@/features/settings/use-settings";
import { AgentIcon } from "@/components/agent-icon";
import { ErrorText } from "@/components/error-text";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

type Props = {
  /** The button that opens the picker. */
  children: ReactNode;
  side?: "top" | "bottom";
};

/**
 * Picks the tutor in a popover: the agents down the left, the shown agent's
 * models in the middle, and the model's reasoning levels on the right. Picking
 * a model or a level of another agent switches the tutor to that agent.
 */
export function TutorPicker({ children, side = "bottom" }: Props) {
  const { settings, change } = useSettings();
  return (
    // Modal, so a click outside only closes the picker, never the Settings dialog it may sit over.
    <Popover modal>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent
        side={side}
        align="start"
        collisionPadding={12}
        aria-label="Tutor"
        className="max-h-(--radix-popover-content-available-height) w-[min(600px,calc(100vw-24px))] gap-0 overflow-hidden p-0 text-base"
      >
        {settings ? <Panel settings={settings} onChange={change} /> : <Message>Loading…</Message>}
      </PopoverContent>
    </Popover>
  );
}

/** What changing the tutor does: the same note whichever agent is shown, here and in Settings. */
export function TutorNote({ className }: { className?: string }) {
  return (
    <p className={className}>
      Changes apply from the tutor's next message. Switching agent starts new conversations; your mission, notes and progress stay. More reasoning gives better
      plans, but replies take longer and use more quota.
    </p>
  );
}

function Panel({ settings, onChange }: { settings: Prefs; onChange: (next: Prefs) => Promise<void> }) {
  const env = useEnvironment();
  const checkingEnv = useCheckingEnvironment();
  const [shown, setShown] = useState(settings.agent);
  const [error, setError] = useState("");

  // Until the first check of what's installed arrives, at least the chosen agent.
  // Agents turned off in Settings are left out.
  const providers = (env?.providers ?? [{ id: settings.agent, name: settings.agent, binary: settings.agent, installed: true }]).filter((p) =>
    agentOn(settings, p.id),
  );
  const provider = providers.find((p) => p.id === shown);

  function choose(agent: string, model: string, effort: string) {
    setError("");
    onChange({ ...settings, agent, models: { ...settings.models, [agent]: { model, effort } } }).catch((e) => setError(errorText(e)));
  }

  function refresh() {
    setError("");
    checkEnvironment().catch((e) => setError(errorText(e)));
  }

  return (
    // Radix's primitives directly: ui/tabs and ui/radio-group draw underlined tabs and radio circles, not a rail and rows.
    <TabsPrimitive.Root orientation="vertical" value={shown} onValueChange={setShown} className="flex min-h-0 flex-1 flex-col">
      {/* 300px tall, shrinking (the columns scroll) when the window has less room. */}
      <div className="flex h-[300px] min-h-0 shrink">
        <div className="flex w-12 flex-none flex-col items-center border-r py-2">
          <TabsPrimitive.List aria-label="Agent" className="flex flex-col items-center gap-1">
            {providers.map((p) => (
              <TabsPrimitive.Trigger
                key={p.id}
                value={p.id}
                aria-label={p.name}
                title={p.installed ? [p.name, p.path].filter(Boolean).join("\n") : `${p.name} (not installed)`}
                className={cn(
                  "relative flex size-8 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-solid data-active:bg-accent data-active:text-foreground",
                  // The agent in use is marked with a bar on the left, like the selected tab.
                  p.id === settings.agent &&
                    "text-foreground before:absolute before:top-1.5 before:bottom-1.5 before:-left-2 before:w-0.5 before:rounded-full before:bg-foreground",
                  !p.installed && "opacity-45",
                )}
              >
                <AgentIcon id={p.id} size={18} />
              </TabsPrimitive.Trigger>
            ))}
          </TabsPrimitive.List>
          <button
            type="button"
            disabled={checkingEnv}
            onClick={refresh}
            aria-label="Refresh installed agents"
            title="Refresh installed agents"
            className="mt-auto flex size-8 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-solid disabled:opacity-50"
          >
            <RefreshCw size={15} aria-hidden="true" className={cn(checkingEnv && "animate-spin")} />
          </button>
        </div>
        {provider && (
          // Not a tab stop: Tab goes from the rail straight to the model list.
          <TabsPrimitive.Content value={provider.id} tabIndex={-1} className="flex min-w-0 flex-1 outline-none">
            <AgentModels
              key={provider.id}
              provider={provider}
              active={provider.id === settings.agent}
              choice={settings.models?.[provider.id] ?? { model: "", effort: "" }}
              onChoose={choose}
            />
          </TabsPrimitive.Content>
        )}
      </div>
      <footer className="flex flex-none flex-col gap-1 border-t px-3 py-2 text-xs text-muted-foreground">
        <TutorNote />
        <ErrorText error={error} />
      </footer>
    </TabsPrimitive.Root>
  );
}

/** One agent's models and the reasoning levels of its chosen model. Only the agent in use shows its choice as ticked. */
function AgentModels({
  provider,
  active,
  choice,
  onChoose,
}: {
  provider: ProviderStatus;
  active: boolean;
  /** The agent's saved model and effort; "" for the agent's default. */
  choice: { model: string; effort: string };
  onChoose: (agent: string, model: string, effort: string) => void;
}) {
  const models = useModels(provider.id);
  const [loadError, setLoadError] = useState("");

  const needsModels = provider.installed && !models && !loadError;
  useEffect(() => {
    if (!needsModels) return;
    let cancelled = false;
    loadModels(provider.id).catch((e) => {
      if (!cancelled) setLoadError(errorText(e));
    });
    return () => {
      cancelled = true;
    };
  }, [needsModels, provider.id, provider.path]);

  if (!provider.installed) {
    return <Message>{provider.name} isn't installed. Install it and sign in, or set where its CLI is in Settings, under Agents.</Message>;
  }
  if (!models) {
    return loadError ? (
      <Message>
        <ErrorText error={loadError} />
      </Message>
    ) : (
      <Message>Loading {provider.name} models…</Message>
    );
  }

  const selected = currentModel(models, choice.model);
  // The chosen model is gone from the list (e.g. retired): still show it, so it can be changed.
  const shownModels: Model[] =
    choice.model && selected?.id !== choice.model ? [...models, { id: choice.model, name: choice.model, description: "", efforts: [] }] : models;
  const model = selected ?? shownModels.find((m) => m.id === choice.model) ?? null;
  // Without a model default (Claude Code's own default model), "Default" lets the CLI decide.
  const efforts = model?.efforts?.length && !model.defaultEffort ? [{ id: "", description: "Let the agent decide" }, ...model.efforts] : (model?.efforts ?? []);
  const effort = choice.effort || model?.defaultEffort || "";

  return (
    <>
      <Column title={provider.name} className="flex-1">
        <RadioGroupPrimitive.Root
          aria-label="Model"
          className="flex flex-col gap-px"
          value={active ? (model?.id ?? null) : null}
          onValueChange={(id) => {
            const m = shownModels.find((m) => m.id === id);
            // Keep the effort if the new model offers it.
            if (m) onChoose(provider.id, m.id, (m.efforts ?? []).some((e) => e.id === choice.effort) ? choice.effort : "");
          }}
        >
          {shownModels.map((m) => (
            <Row key={m.id || "default"} value={m.id} title={m.description ? `${m.name}: ${m.description}` : m.name}>
              {/* The description gives way first, then the name, so the tick stays in view. */}
              <span className="min-w-0 truncate text-foreground">{m.name}</span>
              {m.description && <span className="min-w-0 flex-1 basis-0 truncate text-sm text-muted-foreground">{m.description}</span>}
            </Row>
          ))}
        </RadioGroupPrimitive.Root>
      </Column>

      <Column title="Reasoning" aside={model?.name} className="w-[188px] flex-none border-l">
        {efforts.length === 0 ? (
          <p className="px-2 py-1.5 text-sm text-muted-foreground">This model has no reasoning levels.</p>
        ) : (
          <RadioGroupPrimitive.Root
            aria-label="Reasoning"
            className="flex flex-col gap-px"
            value={active ? effort : null}
            // The saved model, not the one shown: "" keeps following the agent's default model.
            onValueChange={(id) => onChoose(provider.id, choice.model, id)}
          >
            {efforts.map((e) => (
              <Row key={e.id || "default"} value={e.id} title={e.description}>
                <span className="flex-none text-foreground">{e.id ? effortLabel(e.id) : "Default"}</span>
                {e.id && e.id === model?.defaultEffort && <span className="text-sm text-muted-foreground">Default</span>}
              </Row>
            ))}
          </RadioGroupPrimitive.Root>
        )}
      </Column>
    </>
  );
}

/** A scrolling column with a small heading and an optional note on its right. */
function Column({ title, aside, className, children }: { title: string; aside?: string; className?: string; children: ReactNode }) {
  return (
    <section className={cn("flex min-w-0 flex-col", className)}>
      <header className="flex h-9 flex-none items-center justify-between gap-2 px-3 text-sm">
        <h2 className="font-semibold text-foreground">{title}</h2>
        {aside && <span className="min-w-0 truncate text-muted-foreground">{aside}</span>}
      </header>
      <div className="min-h-0 flex-1 overflow-auto px-1.5 pb-2">{children}</div>
    </section>
  );
}

/** A choice as a whole row: its label, then a tick once picked. */
function Row({ value, title, children }: { value: string; title?: string; children: ReactNode }) {
  return (
    <RadioGroupPrimitive.Item
      value={value}
      title={title}
      className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-soft-foreground outline-none hover:bg-accent focus-visible:bg-accent focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring focus-visible:outline-solid data-checked:bg-accent"
    >
      {children}
      <RadioGroupPrimitive.Indicator className="ml-auto flex-none text-foreground">
        <Check size={14} strokeWidth={2.25} aria-hidden="true" />
      </RadioGroupPrimitive.Indicator>
    </RadioGroupPrimitive.Item>
  );
}

function Message({ children }: { children: ReactNode }) {
  return <div className="flex-1 p-4 text-sm text-muted-foreground">{children}</div>;
}
