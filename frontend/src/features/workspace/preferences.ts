import type { UseOpenItemResult } from "@/features/workspace/use-open-item";

/** The id of the plan page's "How you like to learn" section. */
export const PREFERENCES_ID = "how-you-like-to-learn";

/** Opens the plan page scrolled to how the learner likes to learn. */
export function openPreferences(nav: Pick<UseOpenItemResult, "setView">) {
  nav.setView("plan");
  // The plan page renders on this update; scroll once it's there.
  requestAnimationFrame(() => document.getElementById(PREFERENCES_ID)?.scrollIntoView({ block: "start" }));
}
