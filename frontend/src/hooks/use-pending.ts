import { useState } from "react";
import { errorText } from "@/lib/api";

/**
 * State for a submit button. run() marks it pending and clears the error; a
 * failure shows the error and re-enables it. Success leaves it pending on
 * purpose: the next view replaces the form, and the button must not be
 * clickable again while the app catches up.
 */
export function usePending() {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function run(action: () => Promise<unknown>) {
    setPending(true);
    setError("");
    try {
      await action();
    } catch (e) {
      setError(errorText(e));
      setPending(false);
    }
  }
  return { pending, error, setError, run };
}
