// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { PlanView } from "@/features/workspace/plan-view";
import { LiveWorkspaceContext, type LiveWorkspace } from "@/features/workspace/use-workspace";
import { workspace } from "@/features/workspace/fixtures";

const { saveNotes } = vi.hoisted(() => ({ saveNotes: vi.fn(async () => {}) }));
vi.mock("@/lib/api", async (original) => {
  const api = await original<typeof import("@/lib/api")>();
  return { ...api, WorkspaceService: { ...api.WorkspaceService, SaveNotes: saveNotes } };
});

// The real Markdown loads Monaco to colour code, which jsdom can't run.
vi.mock("@/components/markdown", () => ({ Markdown: ({ text }: { text: string }) => <div>{text}</div> }));

afterEach(cleanup);

function show(notes: string) {
  const live = { id: "ws", live: {}, nav: {}, threads: {}, lang: "go" } as unknown as LiveWorkspace;
  return render(
    <LiveWorkspaceContext value={live}>
      <PlanView ws={workspace({ notes })} />
    </LiveWorkspaceContext>,
  );
}

describe("How you like to learn", () => {
  test("shows the notes beside the mission, and saves the learner's edit", async () => {
    show("- Compare Go to Python.\n");
    expect(screen.getByText("- Compare Go to Python.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const field = screen.getByRole("textbox", { name: "How you like to learn" });
    expect((field as HTMLTextAreaElement).value).toBe("- Compare Go to Python.\n");
    fireEvent.change(field, { target: { value: "- Plain words first." } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveNotes).toHaveBeenCalledWith("ws", "- Plain words first."));
    await screen.findByRole("heading", { name: "How you like to learn" }); // back on the plan
  });

  test("says how the notes get written before there are any", () => {
    show("");
    expect(screen.getByText(/Tell it after a lesson or exercise, or write it yourself/)).toBeTruthy();
  });
});
