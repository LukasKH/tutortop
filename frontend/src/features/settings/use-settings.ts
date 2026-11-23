import { createContext, use } from "react";
import type { Prefs } from "@/lib/api";

/** AppRoot, which owns the settings, provides them to every screen once loaded. */
export const SettingsContext = createContext<Prefs | null>(null);

/** The app-wide settings, or null while they load. */
export const useSettings = () => use(SettingsContext);
