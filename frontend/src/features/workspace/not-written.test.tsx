// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { NotWritten } from "@/features/workspace/not-written";

const { stopAgent, writeNow, currentWorking } = vi.hoisted(() => ({
  stopAgent: vi.fn(async (_ws: string, _thread: string) => {}),
  writeNow: vi.fn(async (_ws: string, _objective: string) => {}),
  currentWorking: { value: undefined as { workspace: string; thread: string; thinking: string; preparing: string } | undefined },
}));
vi.mock("@/lib/api", async (original) => {
  const api = await original<typeof import("@/lib/api")>();
  return {
    ...api,
    ChatService: { ...api.ChatService, StopAgent: stopAgent },
    WorkspaceService: { ...api.WorkspaceService, WriteObjectiveNow: writeNow },
  };
});
vi.mock("@/features/workspace/use-workspace", () => ({ useLiveWorkspace: () => ({ id: "ws" }) }));
vi.mock("@/features/workspace/use-working", () => ({ useWorking: () => currentWorking.value }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  currentWorking.value = undefined;
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
  render(page(false, "The writer stopped making progress, so it was stopped."));
  expect(screen.getByText("The writer stopped making progress, so it was stopped.")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
});

test("live writer reasoning shows beneath the status, but duplicate preparing text is omitted", () => {
  currentWorking.value = { workspace: "ws", thread: "write-a", thinking: "Checking Go 1.13 error docs", preparing: "" };
  const { rerender } = render(page(true));
  expect(screen.getByText("Checking Go 1.13 error docs")).toBeTruthy();

  currentWorking.value = { workspace: "ws", thread: "write-a", thinking: "", preparing: "Writing the lesson" };
  rerender(page(true));
  expect(screen.getAllByText("Writing the lesson…")).toHaveLength(1);
});
