import { createContext, use } from "react";

/** The configured tutor, e.g. "Claude Code · Opus · High", and a way to change it. */
export type Tutor = { label: string; openSettings: () => void };

/** AppRoot, which owns the settings, provides the tutor to every screen. */
export const TutorContext = createContext<Tutor>({ label: "", openSettings: () => {} });

export const useTutor = () => use(TutorContext);
