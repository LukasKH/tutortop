// How the app words things.

export function languageLabel(lang: string): string {
  return lang === "go" ? "Go" : lang === "python" ? "Python" : lang.length <= 2 ? lang.toUpperCase() : lang.charAt(0).toUpperCase() + lang.slice(1);
}

export function providerName(id: string): string {
  return id === "claude" ? "Claude Code" : id === "codex" ? "Codex" : id;
}

/** Rough minutes per task size, as the tutor is told to aim for. */
export const TASK_MINUTES: Record<string, number> = {
  small: 10,
  medium: 25,
  large: 60,
};

export const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
