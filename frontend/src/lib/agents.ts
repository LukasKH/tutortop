import { useSyncExternalStore } from "react";
import { SettingsService, type Environment, type Model, type Prefs, type ProviderStatus } from "@/lib/api";

// What's installed and the agents' models, kept for the session and shared by
// Home, Settings and the tutor's label. Names come from the backend, so a new
// agent needs no change here.

/** What's installed as last checked (checkEnvironment), or null before the first check. */
export function useEnvironment(): Environment | null {
  return useSyncExternalStore(subscribe, () => store.env);
}

/** Checks what's installed again. It checks the sandbox too, which takes a few seconds, so useEnvironment keeps the last answer meanwhile. */
export async function checkEnvironment(): Promise<Environment> {
  const env = await SettingsService.Environment();
  update({ env });
  return env;
}

/** The agent's models once loaded (loadModels), else null. */
export function useModels(agent: string): Model[] | null {
  return useSyncExternalStore(subscribe, () => store.models[agent] ?? null);
}

/** Loads the agent's models, once per session: Codex is asked live, which takes a moment. */
export async function loadModels(agent: string): Promise<Model[]> {
  const cached = store.models[agent];
  if (cached) return cached;
  const list = (await SettingsService.Models(agent)) ?? [];
  update({ models: { ...store.models, [agent]: list } });
  return list;
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

/** An agent's name as people know it ("Claude Code"), or its id until the agents are loaded. */
export function providerName(providers: ProviderStatus[], id: string): string {
  return providers.find((p) => p.id === id)?.name ?? id;
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

// The store behind the hooks, as useSyncExternalStore expects: replaced, never
// changed in place, so a hook sees a new value.
let store: { env: Environment | null; models: Record<string, Model[]> } = { env: null, models: {} };
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function update(change: Partial<typeof store>) {
  store = { ...store, ...change };
  listeners.forEach((l) => l());
}
