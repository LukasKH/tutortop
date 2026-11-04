// How the app words things.

/**
 * A language id as people write it ("go" → "Go"). The backend's
 * runner.Toolchain.Name holds this, but only a toolchain check's title carries
 * it to the frontend; the snapshot doesn't. So the built-in languages are
 * named here too.
 */
export function languageLabel(lang: string): string {
  return lang === "go" ? "Go" : lang === "python" ? "Python" : lang.length <= 2 ? lang.toUpperCase() : lang.charAt(0).toUpperCase() + lang.slice(1);
}

/** Rough minutes per task size, as the tutor is told to aim for. */
export const TASK_MINUTES: Record<string, number> = {
  small: 10,
  medium: 25,
  large: 60,
};

export const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
