// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Curriculum } from "@/lib/api";
import { WithPlanSources } from "@/features/workspace/plan-sources";

// Monaco needs a real browser; the markdown here only needs it for colouring code.
vi.mock("@/lib/monaco", () => ({ monaco: { languages: { getLanguages: () => [] }, editor: { colorize: async (code: string) => code } } }));

afterEach(cleanup);

const plan = (references?: { title: string; url: string }[]): Curriculum => ({
  language: "go",
  modules: [{ id: "errors", title: "Errors", references, objectives: [{ id: "go.errors", title: "Wrap errors" }] }],
});

describe("the plan's Sources tab", () => {
  test("keeps the plan in front and shows the module references and resources on request", () => {
    const resources = "# Resources\n\n- [Effective Go](https://go.dev/doc/effective_go): idioms";
    render(
      <WithPlanSources plan={plan([{ title: "Go blog: Working with Errors", url: "https://go.dev/blog/go1.13-errors" }])} resources={resources}>
        <p>The curriculum</p>
      </WithPlanSources>,
    );
    expect(screen.getByText("The curriculum")).toBeTruthy();
    expect(screen.queryByText("Go blog: Working with Errors")).toBeNull();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Sources" }));
    expect(screen.queryByText("The curriculum")).toBeNull();
    expect(screen.getByText("Errors")).toBeTruthy(); // the module the references belong to
    expect(screen.getByRole("link", { name: "Go blog: Working with Errors" }).getAttribute("href")).toBe("https://go.dev/blog/go1.13-errors");
    expect(screen.getByRole("link", { name: "Effective Go" })).toBeTruthy();
  });

  test.each([
    { when: "a plan from before references existed", references: undefined, resources: "" },
    { when: "blank resources", references: [], resources: "  \n" },
  ])("isn't offered without any sources: $when", ({ references, resources }) => {
    render(
      <WithPlanSources plan={plan(references)} resources={resources}>
        <p>The curriculum</p>
      </WithPlanSources>,
    );
    expect(screen.getByText("The curriculum")).toBeTruthy();
    expect(screen.queryByRole("tab")).toBeNull();
  });
});
