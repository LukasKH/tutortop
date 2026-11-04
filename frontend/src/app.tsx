import { useEffect, useState } from "react";
import { App, type Settings } from "@/lib/api";
import { Home } from "@/features/home/home";
import { SettingsDialog } from "@/features/settings/settings-dialog";
import { loadModels, tutorLabel } from "@/lib/models";
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
  const [settings, setSettings] = useState<Settings | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);

  useEffect(() => {
    App.Settings().then((s) => {
      setSettings(s);
      // The tutor label shows model names, which come with the model list.
      loadModels(s.agent)
        .then(() => setSettings((cur) => cur && { ...cur }))
        .catch(() => {});
    });
  }, []);

  async function changeSettings(next: Settings) {
    const prev = settings;
    setSettings(next);
    try {
      await App.SaveSettings(next);
    } catch (e) {
      setSettings(prev);
      throw e;
    }
  }

  const tutor = settings ? tutorLabel(settings) : "";
  const openSettings = () => setSettingsOpen(true);

  return (
    <>
      {workspaceId ? (
        <WorkspaceView key={workspaceId} id={workspaceId} tutor={tutor} onSettings={openSettings} onBack={() => setWorkspaceId(null)} />
      ) : (
        <Home tutor={tutor} lastId={lastId} onSettings={openSettings} onOpen={open} />
      )}
      {settingsOpen && settings && <SettingsDialog settings={settings} onChange={changeSettings} onClose={() => setSettingsOpen(false)} />}
    </>
  );
}
