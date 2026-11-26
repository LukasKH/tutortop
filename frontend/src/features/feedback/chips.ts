import { FeedbackChip } from "@/lib/api";

/** The chips in the order "How was this?" shows them. Each pair's chips say opposite things. */
export const CHIP_PAIRS: FeedbackChip[][] = [
  [FeedbackChip.FeedbackTooTechnical, FeedbackChip.FeedbackClear],
  [FeedbackChip.FeedbackTooLong, FeedbackChip.FeedbackRightLength],
  [FeedbackChip.FeedbackTooEasy, FeedbackChip.FeedbackTooHard],
  [FeedbackChip.FeedbackMoreComparisons],
];

const LABELS: Record<Exclude<FeedbackChip, FeedbackChip.$zero>, string> = {
  [FeedbackChip.FeedbackTooTechnical]: "Too technical",
  [FeedbackChip.FeedbackClear]: "Clear",
  [FeedbackChip.FeedbackTooLong]: "Too long",
  [FeedbackChip.FeedbackRightLength]: "Right length",
  [FeedbackChip.FeedbackTooEasy]: "Too easy",
  [FeedbackChip.FeedbackTooHard]: "Too hard",
  [FeedbackChip.FeedbackMoreComparisons]: "More comparisons to what I know",
};

/** Longer than this, what the learner knows makes a chip too wide, so it keeps the general wording. */
const MAX_KNOWN = 24;

/** A chip's words. Comparisons name what the learner already knows (WorkspaceSnapshot.known), if the mission says. */
export function chipLabel(chip: FeedbackChip, known: string): string {
  const name = known.trim();
  if (chip === FeedbackChip.FeedbackMoreComparisons && name && name.length <= MAX_KNOWN) return `More comparisons to ${name}`;
  return LABELS[chip as keyof typeof LABELS] ?? chip;
}

/** Taps a chip: picks it, dropping the opposite one of its pair, or unpicks it. Keeps the chips in display order. */
export function toggleChip(picked: FeedbackChip[], chip: FeedbackChip): FeedbackChip[] {
  if (picked.includes(chip)) return picked.filter((c) => c !== chip);
  const pair = CHIP_PAIRS.find((p) => p.includes(chip)) ?? [chip];
  const next = [...picked.filter((c) => !pair.includes(c)), chip];
  const order = CHIP_PAIRS.flat();
  return next.sort((a, b) => order.indexOf(a) - order.indexOf(b));
}
