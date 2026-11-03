import { useState, type SetStateAction } from "react";

/** useState that survives reloads, stored in localStorage under "tutortop.<key>". */
export function useStored<T>(key: string, initial: T | (() => T)): [T, (v: SetStateAction<T>) => void] {
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
  const set = (v: SetStateAction<T>) =>
    setValue((prev) => {
      const next = typeof v === "function" ? (v as (prev: T) => T)(prev) : v;
      localStorage.setItem(full, JSON.stringify(next));
      return next;
    });
  return [value, set];
}
