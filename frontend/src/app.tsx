import { useEffect, useState } from "react";
import { SettingsService, type Prefs } from "@/lib/api";
import { Home } from "@/features/home/home";
import { SettingsDialog } from "@/features/settings/settings-dialog";
import { TutorContext } from "@/features/settings/use-tutor";
import { checkEnvironment, loadModels, tutorLabel, useEnvironment, useModels } from "@/lib/agents";
import { WorkspaceView } from "@/features/workspace/workspace-view";
import { useStored } from "@/hooks/use-stored";

export function AppRoot() {
  // Remembered so a reload (or restart) reopens the same workspace.
  const [workspaceId, setWorkspaceId] = useStored<string | null>("workspace", null);
  // The last workspace opened stays in focus on Home.
  const [lastId, setLastId] = useStored<string | null>("last-workspace", null);
  const open = (id: string) => {
    setLastId(id);
    setWorkspaceId(id);
  };
  const [settings, setSettings] = useState<Prefs | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);

  // The tutor's label names the agent and model, which come with these lists.
  const agent = settings?.agent ?? "";
  const providers = useEnvironment()?.providers ?? [];
  const models = useModels(agent);
  useEffect(() => {
    checkEnvironment().catch(() => {});
    SettingsService.Load().then(setSettings);
  }, []);
  useEffect(() => {
    if (agent) loadModels(agent).catch(() => {});
  }, [agent]);

  async function changeSettings(next: Prefs) {
    const prev = settings;
    setSettings(next);
    try {
      await SettingsService.Save(next);
    } catch (e) {
      setSettings(prev);
      throw e;
    }
  }

  const tutor = { label: settings ? tutorLabel(settings, providers, models) : "", openSettings: () => setSettingsOpen(true) };

  return (
    <TutorContext value={tutor}>
      {workspaceId ? <WorkspaceView key={workspaceId} id={workspaceId} onBack={() => setWorkspaceId(null)} /> : <Home lastId={lastId} onOpen={open} />}
      {settingsOpen && settings && <SettingsDialog settings={settings} onChange={changeSettings} onClose={() => setSettingsOpen(false)} />}
    </TutorContext>
  );
}
