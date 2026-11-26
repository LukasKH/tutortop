import { describe, expect, test } from "vitest";
import { FeedbackChip } from "@/lib/api";
import { chipLabel, toggleChip } from "@/features/feedback/chips";

const { FeedbackTooTechnical, FeedbackClear, FeedbackTooLong, FeedbackMoreComparisons } = FeedbackChip;

describe("toggleChip", () => {
  test.each([
    { when: "picks a chip", picked: [], tap: FeedbackClear, want: [FeedbackClear] },
    { when: "unpicks a picked chip", picked: [FeedbackClear], tap: FeedbackClear, want: [] },
    {
      when: "replaces the opposite chip of its pair",
      picked: [FeedbackClear, FeedbackTooLong],
      tap: FeedbackTooTechnical,
      want: [FeedbackTooTechnical, FeedbackTooLong],
    },
    {
      when: "combines chips of different pairs, in display order",
      picked: [FeedbackMoreComparisons],
      tap: FeedbackTooLong,
      want: [FeedbackTooLong, FeedbackMoreComparisons],
    },
  ])("$when", ({ picked, tap, want }) => {
    expect(toggleChip(picked, tap)).toEqual(want);
  });
});

describe("chipLabel", () => {
  test.each([
    { known: "Python", want: "More comparisons to Python" },
    { known: "", want: "More comparisons to what I know" },
    { known: "Python, JavaScript, SQL and some Bash", want: "More comparisons to what I know" },
  ])("comparisons to $known", ({ known, want }) => {
    expect(chipLabel(FeedbackMoreComparisons, known)).toBe(want);
  });

  test("other chips don't depend on what the learner knows", () => {
    expect(chipLabel(FeedbackTooTechnical, "Python")).toBe("Too technical");
  });
});
