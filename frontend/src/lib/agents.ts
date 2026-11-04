import { App, type Model, type ProviderStatus, type Settings } from "@/lib/api";

// The agents the backend offers and their models, for Settings and the
// tutor's label. Names come from the backend, so a new agent needs no change here.

/** The agents and whether each is installed, asked once per session. */
export function loadProviders(): Promise<ProviderStatus[]> {
  if (providers) return Promise.resolve(providers);
  return App.Environment().then((env) => (providers = env.providers ?? []));
}

/** An agent's name as people know it ("Claude Code"), or its id until the agents are loaded. */
export function providerName(id: string): string {
  return providers?.find((p) => p.id === id)?.name ?? id;
}

/** The agents if they're loaded already, else null. */
export const cachedProviders = () => providers;

// Environment also checks the sandbox, which takes a few seconds, so its list of agents is kept.
let providers: ProviderStatus[] | null = null;

/** The agent's models, asked once per session. */
export function loadModels(agent: string): Promise<Model[]> {
  const cached = modelCache.get(agent);
  if (cached) return Promise.resolve(cached);
  return App.Models(agent).then((list) => {
    modelCache.set(agent, list ?? []);
    return list ?? [];
  });
}

/** "Claude Code · Opus · High": the tutor as configured. */
export function tutorLabel(s: Settings): string {
  const choice = s.tutor?.[s.agent] ?? { model: "", effort: "" };
  const model = currentModel(modelCache.get(s.agent) ?? null, choice.model);
  const parts = [providerName(s.agent)];
  if (model?.id || choice.model) parts.push(model?.name ?? choice.model);
  const effort = choice.effort || model?.defaultEffort;
  if (effort) parts.push(effortLabel(effort));
  return parts.join(" · ");
}

/** The agent's models if they're loaded already, else null. */
export const cachedModels = (agent: string) => modelCache.get(agent) ?? null;

// Model lists per agent, kept for the session: Codex is asked live, which takes a moment.
const modelCache = new Map<string, Model[]>();

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
