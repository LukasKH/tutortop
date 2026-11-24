// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { LessonDetail, Source } from "@/lib/api";
import { LessonPanel } from "@/features/lesson/lesson-panel";

const { lessons } = vi.hoisted(() => ({ lessons: new Map<string, unknown>() }));
vi.mock("@/lib/api", async (original) => ({
  ...(await original<typeof import("@/lib/api")>()),
  LessonService: { Lesson: async (_ws: string, id: string) => lessons.get(id) },
}));
// Monaco needs a real browser; the markdown here only needs it for colouring code.
vi.mock("@/lib/monaco", () => ({ monaco: { languages: { getLanguages: () => [] }, editor: { colorize: async (code: string) => code } } }));
vi.mock("@/features/workspace/use-workspace", () => ({ useLiveWorkspace: () => ({ id: "ws" }) }));

afterEach(() => {
  cleanup();
  lessons.clear();
});

function lesson(id: string, sources: Source[] | null): LessonDetail {
  return {
    id,
    title: "Wrapping errors",
    objective: "go.errors",
    questions: 0,
    answered: 0,
    correct: 0,
    createdAt: "",
    summary: "You'll wrap errors.",
    body: "Wrap with `%w`.",
    primarySource: { title: "Go blog: Working with Errors", url: "https://go.dev/blog/go1.13-errors" },
    sources,
    quiz: [],
    results: {},
  };
}

/** Renders a lesson with the module's references and waits for it to load. */
async function show(l: LessonDetail) {
  lessons.set(l.id, l);
  const references = [{ title: "Package errors", url: "https://pkg.go.dev/errors" }];
  render(<LessonPanel lessonId={l.id} moduleTitle="Errors" icon="" lang="go" onOpenExercise={() => {}} exerciseWritten references={references} />);
  await screen.findByText("You'll wrap errors.");
}

const openSources = () => fireEvent.mouseDown(screen.getByRole("tab", { name: "Sources" }));
const linkTo = (name: string) => screen.getByRole("link", { name }).getAttribute("href");

describe("a lesson's Sources tab", () => {
  test("lists what the lesson is based on, what to read next and the module's references, away from the reading", async () => {
    await show(lesson("l1", [{ title: "Go spec: Errors", url: "https://go.dev/ref/spec#Errors" }]));
    expect(screen.queryByRole("link", { name: "Go spec: Errors" })).toBeNull();

    openSources();
    expect(screen.getByText("This lesson is based on")).toBeTruthy();
    expect(linkTo("Go spec: Errors")).toBe("https://go.dev/ref/spec#Errors");
    expect(linkTo("Go blog: Working with Errors")).toBe("https://go.dev/blog/go1.13-errors");
    expect(linkTo("Package errors")).toBe("https://pkg.go.dev/errors");
    expect(screen.queryByText("You'll wrap errors.")).toBeNull();
  });

  test("still works for a lesson written before lessons kept their sources", async () => {
    await show(lesson("l2", null));
    openSources();
    expect(screen.queryByText("This lesson is based on")).toBeNull();
    expect(linkTo("Go blog: Working with Errors")).toBe("https://go.dev/blog/go1.13-errors");
    expect(linkTo("Package errors")).toBe("https://pkg.go.dev/errors");
  });
});
