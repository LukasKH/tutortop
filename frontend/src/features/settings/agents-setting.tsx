import { useEffect, useState } from "react";
import { Dialogs } from "@wailsio/runtime";
import { errorText, type Limit, type ProviderStatus } from "@/lib/api";
import { agentOn, checkAgent, checkEnvironment, useCheck, useChecking, useEnvironment } from "@/lib/agents";
import { useSettings } from "@/features/settings/use-settings";
import { AgentIcon } from "@/components/agent-icon";
import { ErrorText } from "@/components/error-text";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";

/**
 * The agents Tutortop can drive, one row each: version and sign-in, a switch
 * that offers it in the tutor picker, and, opened, its CLI and a check that
 * it works.
 */
export function AgentsSetting() {
  const env = useEnvironment();
  if (!env) return <p className="text-muted-foreground">Loading…</p>;
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Accordion type="single" collapsible className="rounded-lg border">
        {(env.providers ?? []).map((p) => (
          <AgentRow key={p.id} provider={p} />
        ))}
      </Accordion>
      <p className="text-xs text-muted-foreground">
        Tutortop runs each agent's own CLI, signed in with your account. Agents switched off are left out of the tutor picker.
      </p>
    </div>
  );
}

function AgentRow({ provider: p }: { provider: ProviderStatus }) {
  const { settings, change } = useSettings();
  const checked = useCheck(p.id);
  const checking = useChecking(p.id);
  const [error, setError] = useState("");
  const path = p.path ?? "";
  // A check of another CLI (its path changed since) doesn't count.
  const result = checked?.path === path ? checked.result : null;

  function check() {
    setError("");
    checkAgent(p.id, path).catch((e) => setError(errorText(e)));
  }

  // Each installed agent's CLI is checked once a session, and again when it
  // moves. A check that failed waits for "Check again".
  const unchecked = p.installed && !result && !checking && !error;
  useEffect(() => {
    if (!unchecked) return;
    let cancelled = false;
    checkAgent(p.id, path).catch((e) => {
      if (!cancelled) setError(errorText(e));
    });
    return () => {
      cancelled = true;
    };
  }, [unchecked, p.id, path]);

  if (!settings) return null;
  const setup = settings.agents?.[p.id] ?? { off: false, path: "" };
  const inUse = p.id === settings.agent;

  // An arrow, not a function declaration, so it keeps settings known to be loaded.
  const save = async (next: { off?: boolean; path?: string }) => {
    setError("");
    try {
      await change({ ...settings, agents: { ...settings.agents, [p.id]: { ...setup, ...next } } });
      if (next.path !== undefined) await checkEnvironment(); // finds the new CLI, which the effect then checks
    } catch (e) {
      setError(errorText(e));
    }
  };

  function pickCLI() {
    // The system's own file picker. Not available in server mode (a browser).
    Dialogs.OpenFile({
      Title: `Choose the ${p.name} CLI`,
      ButtonText: "Use this CLI",
      Directory: path.replace(/[\\/][^\\/]*$/, "") || undefined, // the CLI's folder
      CanChooseFiles: true,
      CanChooseDirectories: false,
    })
      .then((picked) => (picked ? save({ path: picked }) : undefined))
      .catch((e) => setError(errorText(e)));
  }

  const status = !p.installed ? (
    <span>Not installed</span>
  ) : !result ? (
    <span>{checking ? "Checking…" : error ? "Couldn't check" : ""}</span>
  ) : result.problem ? (
    <span className="text-fail">Needs attention</span>
  ) : (
    <span>{result.account ? `Signed in · ${result.account}` : "Signed in"}</span>
  );
  // A CLI set in Settings that isn't there names that path, not the default.
  const shownPath = !p.installed && setup.path ? `${setup.path} (not found or not executable)` : path || `${p.binary}, not found on PATH`;

  return (
    <AccordionItem value={p.id}>
      <div className="flex items-center gap-3 pr-3">
        <AccordionTrigger className="min-w-0 items-center gap-3 rounded-none py-2.5 pl-3 font-normal hover:no-underline focus-visible:ring-inset">
          <AgentIcon id={p.id} size={18} className={p.installed ? "text-foreground" : "text-muted-foreground"} />
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="flex items-baseline gap-2">
              <span className="text-base text-foreground">{p.name}</span>
              {result?.version && <code className="text-xs text-muted-foreground">v{result.version}</code>}
            </span>
            <span className="truncate text-sm text-muted-foreground">{status}</span>
          </span>
        </AccordionTrigger>
        <Switch
          checked={agentOn(settings, p.id)}
          disabled={inUse}
          onCheckedChange={(on) => save({ off: !on })}
          aria-label={`Offer ${p.name} in the tutor picker`}
          title={inUse ? "The tutor uses this agent. Choose another one to switch it off." : undefined}
        />
      </div>
      <AccordionContent className="flex flex-col gap-2.5 px-3 pt-1 pb-3">
        {result?.problem && <ErrorText error={result.problem} />}
        {result && !result.problem && (
          <p className="text-sm text-soft-foreground">
            Works: signed in and lists {result.models?.length ?? 0} {result.models?.length === 1 ? "model" : "models"}.
          </p>
        )}
        {result?.limits?.map((l, i) => (
          <p key={`${l.windowMinutes}-${i}`} className="text-sm text-soft-foreground">
            {limitText(l)}
          </p>
        ))}
        <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
          <span className="flex min-w-0 flex-col">
            <span className="text-xs text-muted-foreground">CLI</span>
            <code className="min-w-0 truncate" title={shownPath}>
              {shownPath}
            </code>
          </span>
          <span className="flex flex-none gap-1">
            {setup.path && (
              <Button type="button" variant="ghost" onClick={() => save({ path: "" })}>
                Use default
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={pickCLI}>
              Change…
            </Button>
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="sm" disabled={!p.installed || checking} onClick={check}>
            {checking && <Spinner />}
            {checking ? "Checking…" : "Check again"}
          </Button>
          <span className="text-xs text-muted-foreground">Runs the CLI to check its version, sign-in and models.</span>
        </div>
        <ErrorText error={error} />
      </AccordionContent>
    </AccordionItem>
  );
}

/** "5-hour limit: 12% used, resets 14:30". */
function limitText(l: Limit): string {
  const parsed = l.resetsAt ? new Date(l.resetsAt) : null;
  const remaining = parsed && Number.isFinite(parsed.getTime()) ? parsed.getTime() - Date.now() : 0;
  // Within a day, the time says when; further off, the date; once past, omit it.
  const when =
    parsed && remaining > 0
      ? remaining < 24 * 60 * 60 * 1000
        ? parsed.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
        : parsed.toLocaleDateString(undefined, { month: "long", day: "numeric" })
      : "";
  return `${windowName(l.windowMinutes)} limit: ${Math.round(l.usedPercent)}% used${when ? `, resets ${when}` : ""}`;
}

/** A usage limit's period in words: "5-hour", "Weekly". */
function windowName(minutes: number): string {
  if (minutes <= 0) return "Usage";
  if (minutes < 60) return `${minutes}-minute`;
  if (minutes === 7 * 24 * 60) return "Weekly";
  if (minutes >= 28 * 24 * 60 && minutes <= 31 * 24 * 60) return "Monthly";
  if (minutes % (24 * 60) === 0) return `${minutes / (24 * 60)}-day`;
  return `${Math.round(minutes / 60)}-hour`;
}
