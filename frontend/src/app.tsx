import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppEvents, SettingsService, useEvent, type Prefs } from "@/lib/api";
import { Home } from "@/features/home/home";
import { SettingsDialog } from "@/features/settings/settings-dialog";
import { SettingsContext, type SettingsPage } from "@/features/settings/use-settings";
import { checkEnvironment, loadModels } from "@/lib/agents";
import { applyUpdate, loadUpdate } from "@/lib/update";
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
  const [settingsPage, setSettingsPage] = useState<SettingsPage | null>(null);
  // Saves are numbered. Once none is in flight, the settings shown are the
  // newest the backend took: a failed save rolls back, but not past a later
  // save, or an earlier one that succeeded after it.
  const saves = useRef(0);
  const inFlight = useRef(new Set<number>());
  const saved = useRef<{ seq: number; prefs: Prefs | null }>({ seq: 0, prefs: null });

  const agent = settings?.agent ?? "";
  useEvent(AppEvents.UpdateChanged, applyUpdate);
  useEffect(() => {
    const controller = new AbortController();
    checkEnvironment().catch(() => {});
    loadUpdate().catch(() => {});
    SettingsService.Load().then((prefs) => {
      if (controller.signal.aborted || saves.current > 0) return;
      saved.current = { seq: 0, prefs };
      setSettings(prefs);
    });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (agent) loadModels(agent).catch(() => {});
  }, [agent]);

  const changeSettings = useCallback(async (next: Prefs) => {
    const seq = ++saves.current;
    inFlight.current.add(seq);
    setSettings(next);
    try {
      await SettingsService.Save(next);
      if (seq > saved.current.seq) saved.current = { seq, prefs: next };
    } finally {
      inFlight.current.delete(seq);
      if (inFlight.current.size === 0 && saved.current.seq !== saves.current) setSettings(saved.current.prefs);
    }
  }, []);

  const openSettings = useCallback((page: SettingsPage = "tutor") => setSettingsPage(page), []);
  const context = useMemo(() => ({ settings, change: changeSettings, openSettings }), [settings, changeSettings, openSettings]);

  return (
    <SettingsContext value={context}>
      {workspaceId ? <WorkspaceView key={workspaceId} id={workspaceId} onBack={() => setWorkspaceId(null)} /> : <Home lastId={lastId} onOpen={open} />}
      {settingsPage && <SettingsDialog initialPage={settingsPage} onClose={() => setSettingsPage(null)} />}
    </SettingsContext>
  );
}
