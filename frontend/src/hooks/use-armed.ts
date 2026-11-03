import { useEffect, useState } from "react";

/** A two-click confirmation that disarms itself after a few seconds. */
export function useArmed(): [boolean, () => void] {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const t = window.setTimeout(() => setArmed(false), 3500);
    return () => window.clearTimeout(t);
  }, [armed]);
  return [armed, () => setArmed(true)];
}
