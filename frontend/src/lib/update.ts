import { useSyncExternalStore } from "react";
import { Phase, UpdateService, type State } from "@/lib/api";

// Session-wide app update state, kept in sync with UpdateService and shared by
// Home, the workspace sidebar and Settings.

/** The current updater state, or null before the initial Status call finishes. */
export function useUpdate(): State | null {
  return useSyncExternalStore(subscribe, () => store);
}

/** Loads the initial updater state. Ignored if a live event has already arrived. */
export async function loadUpdate(): Promise<State> {
  const next = await UpdateService.Status();
  if (store === null) set(next);
  return next;
}

/** Applies a state pushed by the backend's update:changed event. */
export function applyUpdate(next: State) {
  set(next);
}

/** Registers a hook (such as flushing pending editor saves) to await before restarting. */
export function registerBeforeRestart(fn: () => Promise<void>): () => void {
  beforeRestart.add(fn);
  return () => {
    beforeRestart.delete(fn);
  };
}

/** Flushes pending saves, then hands the staged update to the swap helper and restarts. */
export async function restartToUpdate(): Promise<void> {
  await Promise.allSettled([...beforeRestart].map((fn) => fn()));
  return UpdateService.Restart();
}

/** Short label for the subtle header and sidebar update controls, or "" when nothing needs attention. */
export function updateNoticeLabel(state: State | null): string {
  if (!state) return "";
  switch (state.phase) {
    case Phase.PhaseAvailable:
      return state.version ? `Update ${state.version}` : "Update available";
    case Phase.PhaseDownloading: {
      const pct = downloadPercent(state);
      if (pct === 100) return "Preparing…";
      return pct !== null ? `Downloading ${pct}%` : "Downloading…";
    }
    case Phase.PhaseReady:
      return "Restart to update";
    case Phase.PhaseRestarting:
      return "Restarting…";
    default:
      return "";
  }
}

/** Download percentage (0–100), or null when the total size isn't known yet. */
export function downloadPercent(state: State): number | null {
  const total = state.total ?? 0;
  if (total <= 0) return null;
  const written = Math.max(0, state.written ?? 0);
  return Math.min(100, Math.round((written / total) * 100));
}

let store: State | null = null;
const listeners = new Set<() => void>();
const beforeRestart = new Set<() => Promise<void>>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function set(next: State) {
  store = next;
  listeners.forEach((l) => l());
}
