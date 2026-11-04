import { useEffect, useState } from "react";

/** useState that survives reloads, stored in localStorage under "tutortop.<key>". */
export function useStored<T>(key: string, initial: T | (() => T)) {
  const full = `tutortop.${key}`;
  const [value, setValue] = useState<T>(() => readStored(key, initial));
  useEffect(() => localStorage.setItem(full, JSON.stringify(value)), [full, value]);
  return [value, setValue] as const;
}

/**
 * What useStored keeps under key, else initial. As useStored's initial, it
 * moves a value from a key that's been renamed.
 */
export function readStored<T>(key: string, initial: T | (() => T)): T {
  const raw = localStorage.getItem(`tutortop.${key}`);
  if (raw !== null) {
    try {
      return JSON.parse(raw) as T;
    } catch {
      /* fall through to the default */
    }
  }
  return typeof initial === "function" ? (initial as () => T)() : initial;
}
