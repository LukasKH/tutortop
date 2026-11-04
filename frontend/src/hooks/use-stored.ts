import { useEffect, useState } from "react";

/** useState that survives reloads, stored in localStorage under "tutortop.<key>". */
export function useStored<T>(key: string, initial: T | (() => T)) {
  const full = `tutortop.${key}`;
  const [value, setValue] = useState<T>(() => {
    const raw = localStorage.getItem(full);
    if (raw !== null) {
      try {
        return JSON.parse(raw) as T;
      } catch {
        /* fall through to the default */
      }
    }
    return typeof initial === "function" ? (initial as () => T)() : initial;
  });
  useEffect(() => localStorage.setItem(full, JSON.stringify(value)), [full, value]);
  return [value, setValue] as const;
}
