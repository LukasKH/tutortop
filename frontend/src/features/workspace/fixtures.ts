import type { LessonStatus, TaskStatus, WorkspaceSnapshot } from "@/lib/api";

// Workspace snapshots for tests: only what a test sets differs from an empty workspace.

/** A workspace with a plan of objectives "a", "b" and "c" in one module, in Go. */
export function workspace(over: Partial<WorkspaceSnapshot> = {}): WorkspaceSnapshot {
  return {
    id: "ws",
    title: "Go",
    icon: "",
    mission: "",
    curriculum: {
      language: "go",
      modules: [{ id: "m1", title: "Basics", objectives: ["a", "b", "c"].map((id) => ({ id, title: `Objective ${id}` })) }],
    },
    proposal: null,
    progress: { passed: 0, total: 3, passedEver: 0, objectives: {} },
    tasks: [],
    lessons: [],
    questions: null,
    busy: {},
    checks: {},
    seq: 1,
    ...over,
  };
}

export function task(id: string, objective: string, passed = false): TaskStatus {
  return { id, title: `Task ${id}`, objective, language: "go", size: "small", files: [], createdAt: "", passed, hintLevel: 0 };
}

export function lesson(id: string, objective: string, answered = 0, questions = 2): LessonStatus {
  return { id, title: `Lesson ${id}`, objective, questions, answered, correct: 0, createdAt: "" };
}
