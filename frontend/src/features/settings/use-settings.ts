import { createContext, use } from "react";
import type { Prefs } from "@/lib/api";

/**
 * The app-wide settings, or null while they load; change, which saves new
 * settings; and a way to open Settings.
 */
export type Settings = { settings: Prefs | null; change: (next: Prefs) => Promise<void>; openSettings: () => void };

/** AppRoot, which owns the settings, provides them to every screen. */
export const SettingsContext = createContext<Settings>({ settings: null, change: async () => {}, openSettings: () => {} });

export const useSettings = () => use(SettingsContext);
