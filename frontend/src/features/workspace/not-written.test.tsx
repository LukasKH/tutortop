// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { NotWritten } from "@/features/workspace/not-written";

const { stopAgent, writeNow } = vi.hoisted(() => ({
  stopAgent: vi.fn(async (_ws: string, _thread: string) => {}),
  writeNow: vi.fn(async (_ws: string, _objective: string) => {}),
}));
vi.mock("@/lib/api", async (original) => {
  const api = await original<typeof import("@/lib/api")>();
  return {
    ...api,
    ChatService: { ...api.ChatService, StopAgent: stopAgent },
    WorkspaceService: { ...api.WorkspaceService, WriteObjectiveNow: writeNow },
  };
});
vi.mock("@/features/workspace/use-workspace", () => ({ useLiveWorkspace: () => ({ id: "ws", working: {} }) }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function page(writing: boolean, failed = "") {
  return (
    <NotWritten
      objective={{ id: "a", name: "", title: "Wrapping errors" }}
      part="lesson"
      moduleTitle="Errors"
      writing={writing}
      check={null}
      failed={failed}
      other={null}
    />
  );
}

test("a writer at work can be stopped, then written again", async () => {
  const { rerender } = render(page(true));
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Stop" })));
  expect(stopAgent).toHaveBeenCalledWith("ws", "write-a");

  // The writer ends: the backend's thread:busy turns writing off.
  rerender(page(false));
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
  expect(screen.getByText("The lesson didn't get written. Try again.")).toBeTruthy();
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Try again" })));
  expect(writeNow).toHaveBeenCalledWith("ws", "a");
});

test("a writer stopped for making no progress says why", () => {
  render(page(false, "The writer stopped making progress, so it was stopped. Try again."));
  expect(screen.getByText("The writer stopped making progress, so it was stopped. Try again.")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
});
