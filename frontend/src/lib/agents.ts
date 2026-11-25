import { useSyncExternalStore } from "react";
import { SettingsService, type Check, type Environment, type Model, type Prefs, type ProviderStatus } from "@/lib/api";

// What's installed and the agents' models, kept for the session and shared by
// Home, Settings and the tutor's label. Names come from the backend, so a new
// agent needs no change here.

/** What's installed as last checked (checkEnvironment), or null before the first check. */
export function useEnvironment(): Environment | null {
  return useSyncExternalStore(subscribe, () => store.env);
}

/** Whether checkEnvironment is currently running. */
export function useCheckingEnvironment(): boolean {
  return useSyncExternalStore(subscribe, () => store.checkingEnv);
}

/** Checks what's installed again. It checks the sandbox too, which takes a few seconds, so useEnvironment keeps the last answer meanwhile. */
export async function checkEnvironment(): Promise<Environment> {
  const seq = ++envSeq;
  update({ checkingEnv: true });
  try {
    const env = await SettingsService.Environment();
    if (seq !== envSeq) return store.env ?? env;
    // Models and checks from a CLI that has moved since may not be the new CLI's.
    const moved = (store.env?.providers ?? []).filter((p) => env.providers?.find((n) => n.id === p.id)?.path !== p.path).map((p) => p.id);
    const nextChecking = moved.length === 0 ? store.checking : { ...store.checking };
    for (const a of moved) {
      checking.delete(a);
      nextChecking[a] = false;
    }
    update({ env, checkingEnv: false, models: withoutModels(store.models, moved), checking: nextChecking });
    return env;
  } catch (e) {
    if (seq === envSeq) update({ checkingEnv: false });
    throw e;
  }
}

let envSeq = 0;

/** Whether env lacks something Tutortop needs, as SetupDialog lists: an agent to tutor, or Docker or Podman for checks. */
export function needsSetup(env: Environment): boolean {
  return !hasAgent(env) || !env.sandboxOk;
}

/** Whether any agent's CLI is installed, so there can be a tutor. */
export function hasAgent(env: Environment): boolean {
  return (env.providers ?? []).some((p) => p.installed);
}

/** The agent's models once loaded (loadModels), else null. */
export function useModels(agent: string): Model[] | null {
  return useSyncExternalStore(subscribe, () => store.models[agent] ?? null);
}

/**
 * Loads the agent's models, once per session: Codex is asked live, which takes
 * a moment, so callers asking meanwhile share the one request.
 */
export function loadModels(agent: string): Promise<Model[]> {
  const cached = store.models[agent];
  if (cached) return Promise.resolve(cached);
  const running = loading.get(agent);
  if (running) return running;
  const request: Promise<Model[]> = SettingsService.Models(agent)
    .then((list) => {
      // Not when the models were dropped meanwhile (withoutModels): the list may be the old CLI's.
      if (loading.get(agent) === request) update({ models: { ...store.models, [agent]: list ?? [] } });
      return list ?? [];
    })
    .finally(() => {
      if (loading.get(agent) === request) loading.delete(agent);
    });
  loading.set(agent, request);
  return request;
}

const loading = new Map<string, Promise<Model[]>>();

/** An agent's check, and the CLI it tried. */
export type Checked = { path: string; result: Check };

/** The agent's last check (checkAgent) while the app runs, or null before one. */
export function useCheck(agent: string): Checked | null {
  return useSyncExternalStore(subscribe, () => store.checks[agent] ?? null);
}

/** Whether the agent's check is running. */
export function useChecking(agent: string): boolean {
  return useSyncExternalStore(subscribe, () => store.checking[agent] ?? false);
}

/**
 * Tries the agent's CLI, found at path: version, sign-in and models. The
 * models it lists replace the loaded ones, as the CLI may have changed; a
 * problem drops them. Callers asking meanwhile share the one request, and a
 * check of a newer path supersedes it.
 */
export function checkAgent(agent: string, path: string): Promise<Check> {
  const running = checking.get(agent);
  if (running?.path === path) return running.request;
  update({ checking: { ...store.checking, [agent]: true } });
  const request: Promise<Check> = SettingsService.Check(agent)
    .then((result) => {
      if (checking.get(agent)?.request === request) {
        if (!result.problem) loading.delete(agent); // an older loadModels in flight mustn't overwrite these
        update({
          checks: { ...store.checks, [agent]: { path, result } },
          models: result.problem ? withoutModels(store.models, [agent]) : { ...store.models, [agent]: result.models ?? [] },
        });
      }
      return result;
    })
    .finally(() => {
      if (checking.get(agent)?.request !== request) return;
      checking.delete(agent);
      update({ checking: { ...store.checking, [agent]: false } });
    });
  checking.set(agent, { path, request });
  return request;
}

const checking = new Map<string, { path: string; request: Promise<Check> }>();

/** Whether the agent is offered in the tutor picker: on in the settings, or the tutor's agent. */
export function agentOn(s: Prefs, agent: string): boolean {
  return agent === s.agent || !s.agents?.[agent]?.off;
}

/** "Claude Code · Opus · High": the tutor as configured. */
export function tutorLabel(s: Prefs, providers: ProviderStatus[], models: Model[] | null): string {
  const choice = s.models?.[s.agent] ?? { model: "", effort: "" };
  const model = currentModel(models, choice.model);
  const parts = [providerName(providers, s.agent)];
  if (model?.id || choice.model) parts.push(model?.name ?? choice.model);
  const effort = choice.effort || model?.defaultEffort;
  if (effort) parts.push(effortLabel(effort));
  return parts.join(" · ");
}

/**
 * The chosen model. No choice means the agent's default: Claude Code lists
 * that as its own entry; for Codex it's the default model, listed first.
 */
export function currentModel(models: Model[] | null, id: string): Model | null {
  if (!models) return null;
  return models.find((m) => m.id === id) ?? (id === "" ? (models[0] ?? null) : null);
}

export function effortLabel(id: string) {
  return EFFORT_LABELS[id] ?? id.charAt(0).toUpperCase() + id.slice(1);
}

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

/** An agent's name as people know it ("Claude Code"), or its id until the agents are loaded. */
function providerName(providers: ProviderStatus[], id: string): string {
  return providers.find((p) => p.id === id)?.name ?? id;
}

// The store behind the hooks, as useSyncExternalStore expects: replaced, never
// changed in place, so a hook sees a new value.
let store: {
  env: Environment | null;
  checkingEnv: boolean;
  models: Record<string, Model[]>;
  checks: Record<string, Checked>;
  checking: Record<string, boolean>;
} = { env: null, checkingEnv: false, models: {}, checks: {}, checking: {} };
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** The models without the agents', and their loads in flight, so they're asked again. */
function withoutModels(models: Record<string, Model[]>, agents: string[]): Record<string, Model[]> {
  if (agents.length === 0) return models;
  agents.forEach((a) => loading.delete(a));
  return Object.fromEntries(Object.entries(models).filter(([a]) => !agents.includes(a)));
}

function update(change: Partial<typeof store>) {
  store = { ...store, ...change };
  listeners.forEach((l) => l());
}
