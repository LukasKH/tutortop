import { describe, expect, test } from "vitest";
import { FeedbackChip, ObjectiveStatus } from "@/lib/api";
import { chipLabel, toggleChip, unsolvedWrittenCount } from "@/features/feedback/chips";
import { lesson, task, workspace } from "@/features/workspace/fixtures";

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

describe("unsolvedWrittenCount", () => {
  test("counts unpassed objectives with written lessons or tasks and excludes passed ones and keepLessonId", () => {
    const ws = workspace({
      curriculum: {
        language: "go",
        modules: [{ id: "m1", title: "Basics", objectives: ["a", "b", "c", "d"].map((id) => ({ id, title: `Objective ${id}` })) }],
      },
      progress: {
        passed: 1,
        total: 4,
        passedEver: 1,
        objectives: {
          a: { id: "a", status: ObjectiveStatus.ObjectivePassed, tasks: ["0001-a"] },
          b: { id: "b", status: ObjectiveStatus.ObjectivePlanned, tasks: [] },
          c: { id: "c", status: ObjectiveStatus.ObjectiveInProgress, tasks: ["0003-c"] },
          d: { id: "d", status: ObjectiveStatus.ObjectivePlanned, tasks: [] },
        },
      },
      lessons: [lesson("0001-a", "a"), lesson("0002-b", "b"), lesson("0003-c", "c")],
      tasks: [task("0001-a", "a", true), task("0003-c", "c")],
    });
    expect(unsolvedWrittenCount(ws)).toBe(2); // b (lesson) and c (lesson+task)
    expect(unsolvedWrittenCount(ws, "0002-b")).toBe(1); // b's only written item is the kept lesson; c still counts
  });
});
