import { expect, test, vi } from "vitest";
import type { Environment } from "@/lib/api";
import { loadProviders, providerName } from "@/lib/agents";

const { environment } = vi.hoisted(() => ({ environment: vi.fn<() => Promise<Partial<Environment>>>() }));
vi.mock("@/lib/api", () => ({ SettingsService: { Environment: environment } }));

test("an agent is named by the backend once loaded, by its id until then", async () => {
  environment.mockResolvedValue({ providers: [{ id: "claude", name: "Claude Code", installed: true }] });
  expect(providerName("claude")).toBe("claude");
  await loadProviders();
  expect(providerName("claude")).toBe("Claude Code");
  expect(providerName("other")).toBe("other");
  // Asked once: Environment also checks the sandbox.
  await loadProviders();
  expect(environment).toHaveBeenCalledTimes(1);
});
