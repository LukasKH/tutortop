// @vitest-environment jsdom
import { beforeEach, expect, test, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { Check, Environment, Model, Prefs } from "@/lib/api";

// The backend's settings service, faked. Each test loads a fresh agents module, so its session cache starts empty.
const { service } = vi.hoisted(() => ({
  service: { Models: vi.fn(), Check: vi.fn(), Environment: vi.fn() },
}));
vi.mock("@/lib/api", () => ({ SettingsService: service }));

let agents: typeof import("@/lib/agents");

beforeEach(async () => {
  vi.resetModules();
  vi.resetAllMocks();
  agents = await import("@/lib/agents");
});

const model = (id: string): Model => ({ id, name: id, efforts: [] });
const env = (path: string): Environment => ({
  providers: [{ id: "codex", name: "Codex", binary: "codex", installed: true, path }],
  sandboxOk: true,
  root: "",
  defaultRoot: "",
  rootFromEnv: false,
});
const prefs = (over: Partial<Prefs> = {}): Prefs => ({ agent: "codex", models: null, agents: null, copilot: false, ...over });

test("models load once per agent, and callers asking meanwhile share the request", async () => {
  service.Models.mockImplementation(async (agent: string) => [model(`${agent}-1`)]);
  const [a, b] = await Promise.all([agents.loadModels("codex"), agents.loadModels("codex")]);
  expect(a).toEqual([model("codex-1")]);
  expect(b).toBe(a);
  expect(await agents.loadModels("codex")).toBe(a);
  expect(await agents.loadModels("claude")).toEqual([model("claude-1")]);
  expect(service.Models).toHaveBeenCalledTimes(2);
});

test("a check runs once for callers asking meanwhile, and its models replace the loaded ones", async () => {
  service.Models.mockResolvedValue([model("old")]);
  await agents.loadModels("codex");
  let answer!: (c: Check) => void;
  service.Check.mockReturnValue(new Promise<Check>((r) => (answer = r)));
  const checking = renderHook(() => agents.useChecking("codex"));

  let first!: Promise<Check>;
  let second!: Promise<Check>;
  act(() => {
    first = agents.checkAgent("codex", "/bin/codex");
    second = agents.checkAgent("codex", "/bin/codex");
  });
  expect(second).toBe(first);
  expect(service.Check).toHaveBeenCalledTimes(1);
  expect(checking.result.current).toBe(true);

  await act(async () => {
    answer({ models: [model("new")] });
    await first;
  });
  expect(checking.result.current).toBe(false);
  expect(await agents.loadModels("codex")).toEqual([model("new")]);
  expect(service.Models).toHaveBeenCalledTimes(1);
});

test("a check with a problem drops the agent's models, so they're asked again", async () => {
  service.Models.mockResolvedValue([model("old")]);
  await agents.loadModels("codex");
  service.Check.mockResolvedValue({ problem: "not signed in" });
  await agents.checkAgent("codex", "/bin/codex");

  const models = renderHook(() => agents.useModels("codex"));
  expect(models.result.current).toBeNull();
  service.Models.mockResolvedValue([model("again")]);
  expect(await agents.loadModels("codex")).toEqual([model("again")]);
});

test("models are dropped when the agent's CLI moves", async () => {
  service.Environment.mockResolvedValue(env("/old/codex"));
  await agents.checkEnvironment();
  service.Models.mockResolvedValue([model("old")]);
  await agents.loadModels("codex");

  await agents.checkEnvironment(); // same CLI: kept
  expect(await agents.loadModels("codex")).toEqual([model("old")]);

  service.Environment.mockResolvedValue(env("/new/codex"));
  await agents.checkEnvironment();
  service.Models.mockResolvedValue([model("new")]);
  expect(await agents.loadModels("codex")).toEqual([model("new")]);
  expect(service.Models).toHaveBeenCalledTimes(2);
});

test("out-of-order checkEnvironment replies don't overwrite a newer path, and moving cancels in-flight probes", async () => {
  service.Environment.mockResolvedValueOnce(env("/old/codex"));
  await agents.checkEnvironment();

  let resolveCheck!: (c: Check) => void;
  service.Check.mockReturnValueOnce(new Promise<Check>((r) => (resolveCheck = r)));
  let resolveModels!: (m: Model[]) => void;
  service.Models.mockReturnValueOnce(new Promise<Model[]>((r) => (resolveModels = r)));

  const checking = renderHook(() => agents.useChecking("codex"));
  const models = renderHook(() => agents.useModels("codex"));
  let checkPromise!: Promise<Check>;
  let loadPromise!: Promise<Model[]>;
  act(() => {
    checkPromise = agents.checkAgent("codex", "/old/codex");
    loadPromise = agents.loadModels("codex");
  });
  expect(checking.result.current).toBe(true);

  let resolveSlow!: (e: Environment) => void;
  let resolveFast!: (e: Environment) => void;
  service.Environment.mockReturnValueOnce(new Promise<Environment>((r) => (resolveSlow = r))).mockReturnValueOnce(
    new Promise<Environment>((r) => (resolveFast = r)),
  );
  const slow = agents.checkEnvironment();
  const fast = agents.checkEnvironment();

  // Newer reply: CLI moved to a broken path (no longer installed).
  await act(async () => {
    resolveFast({ ...env(""), providers: [{ id: "codex", name: "Codex", binary: "codex", installed: false, path: "" }] });
    await fast;
  });
  expect(checking.result.current).toBe(false);

  // Older checkEnvironment, checkAgent, and loadModels resolving later must not write back stale data.
  await act(async () => {
    resolveSlow(env("/old/codex"));
    resolveCheck({ models: [model("stale-check")] });
    resolveModels([model("stale-load")]);
    await Promise.all([slow, checkPromise, loadPromise]);
  });
  expect(renderHook(() => agents.useEnvironment()).result.current?.providers?.[0]?.path).toBe("");
  expect(models.result.current).toBeNull();
});

test("a newer checkAgent supersedes an older one for another path", async () => {
  let resolveOld!: (c: Check) => void;
  let resolveNew!: (c: Check) => void;
  service.Check.mockReturnValueOnce(new Promise<Check>((r) => (resolveOld = r))).mockReturnValueOnce(new Promise<Check>((r) => (resolveNew = r)));
  const checking = renderHook(() => agents.useChecking("codex"));
  const models = renderHook(() => agents.useModels("codex"));

  let oldP!: Promise<Check>;
  let newP!: Promise<Check>;
  act(() => {
    oldP = agents.checkAgent("codex", "/old/codex");
    newP = agents.checkAgent("codex", "/new/codex");
  });

  await act(async () => {
    resolveOld({ models: [model("old")] });
    await oldP;
  });
  expect(checking.result.current).toBe(true);
  expect(models.result.current).toBeNull();

  await act(async () => {
    resolveNew({ models: [model("new")] });
    await newP;
  });
  expect(checking.result.current).toBe(false);
  expect(models.result.current).toEqual([model("new")]);
});

test("agents switched off leave the picker, except the tutor's own", () => {
  const s = prefs({ agents: { claude: { off: true, path: "" }, codex: { off: true, path: "" } } });
  expect(agents.agentOn(s, "claude")).toBe(false);
  expect(agents.agentOn(s, "codex")).toBe(true);
  expect(agents.agentOn(s, "antigravity")).toBe(true);
});

test("the tutor's label names the agent, model and reasoning", () => {
  const providers = env("/bin/codex").providers!;
  const models: Model[] = [
    { id: "gpt", name: "GPT", efforts: [], defaultEffort: "medium" },
    { id: "mini", name: "Mini", efforts: [] },
  ];
  expect(agents.tutorLabel(prefs({ models: { codex: { model: "mini", effort: "xhigh" } } }), providers, models)).toBe("Codex · Mini · Extra high");
  // No choice: the agent's default model and its default effort.
  expect(agents.tutorLabel(prefs(), providers, models)).toBe("Codex · GPT · Medium");
  // Before the lists load: the ids as saved.
  expect(agents.tutorLabel(prefs({ models: { codex: { model: "mini", effort: "" } } }), [], null)).toBe("codex · mini");
});
