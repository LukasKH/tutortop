import { tutorLabel, useEnvironment, useModels } from "@/lib/agents";
import { useSettings } from "@/features/settings/use-settings";

/** The configured tutor: its agent, and its label, e.g. "Claude Code · Opus · High". Both are "" while the settings load. */
export type Tutor = { agent: string; label: string };

export function useTutor(): Tutor {
  const { settings } = useSettings();
  const agent = settings?.agent ?? "";
  // The label names the agent and model, which come with these lists.
  const providers = useEnvironment()?.providers ?? [];
  const models = useModels(agent);
  return { agent, label: settings ? tutorLabel(settings, providers, models) : "" };
}
