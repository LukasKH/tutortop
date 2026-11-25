// @vitest-environment jsdom
import { beforeEach, expect, test, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { Phase, type State } from "@/lib/api";

const { service } = vi.hoisted(() => ({
  service: { Status: vi.fn(), Check: vi.fn(), Install: vi.fn(), Restart: vi.fn() },
}));
vi.mock("@/lib/api", async (original) => ({
  ...(await original<typeof import("@/lib/api")>()),
  UpdateService: service,
}));

let update: typeof import("@/lib/update");

beforeEach(async () => {
  vi.resetModules();
  vi.resetAllMocks();
  update = await import("@/lib/update");
});

const state = (over: Partial<State> = {}): State => ({
  currentVersion: "0.2.5",
  phase: Phase.PhaseIdle,
  canSelfUpdate: true,
  releaseUrl: "https://github.com/LukasKH/tutortop/releases/latest",
  ...over,
});

test("live events aren't overwritten by a slower initial Status response", async () => {
  let resolveInitial!: (s: State) => void;
  service.Status.mockReturnValue(new Promise<State>((r) => (resolveInitial = r)));

  const hook = renderHook(() => update.useUpdate());
  const load = update.loadUpdate();

  const live = state({ phase: Phase.PhaseAvailable, version: "0.3.0" });
  act(() => {
    update.applyUpdate(live);
  });
  expect(hook.result.current).toEqual(live);

  await act(async () => {
    resolveInitial(state({ phase: Phase.PhaseIdle }));
    await load;
  });
  expect(hook.result.current).toEqual(live);
});

test("notice label covers actionable phases, failed downloads and progress", () => {
  expect(update.updateNoticeLabel(null)).toBe("");
  expect(update.updateNoticeLabel(state({ phase: Phase.PhaseUpToDate }))).toBe("");

  const avail = state({ phase: Phase.PhaseAvailable, version: "0.3.0" });
  expect(update.updateNoticeLabel(avail)).toBe("Update 0.3.0");
  // Failed downloads keep PhaseAvailable + Error, so the notice stays visible.
  expect(update.updateNoticeLabel(state({ phase: Phase.PhaseAvailable, version: "0.3.0", error: "network lost" }))).toBe("Update 0.3.0");

  const dl = state({ phase: Phase.PhaseDownloading, written: 5 * 1024 * 1024, total: 10 * 1024 * 1024 });
  expect(update.downloadPercent(dl)).toBe(50);
  expect(update.updateNoticeLabel(dl)).toBe("Downloading 50%");

  const done = state({ phase: Phase.PhaseDownloading, written: 10, total: 10 });
  expect(update.updateNoticeLabel(done)).toBe("Preparing…");

  expect(update.updateNoticeLabel(state({ phase: Phase.PhaseReady, version: "0.3.0" }))).toBe("Restart to update");
});

test("restartToUpdate awaits registered before-restart flushes before restarting", async () => {
  const order: string[] = [];
  let finishFlush!: () => void;
  const unregister = update.registerBeforeRestart(
    () =>
      new Promise<void>((resolve) => {
        order.push("flush:start");
        finishFlush = () => {
          order.push("flush:done");
          resolve();
        };
      }),
  );
  service.Restart.mockImplementation(async () => {
    order.push("restart");
  });

  const restart = update.restartToUpdate();
  expect(order).toEqual(["flush:start"]);
  expect(service.Restart).not.toHaveBeenCalled();

  finishFlush();
  await restart;
  expect(order).toEqual(["flush:start", "flush:done", "restart"]);

  unregister();
  await update.restartToUpdate();
  expect(service.Restart).toHaveBeenCalledTimes(2);
});
