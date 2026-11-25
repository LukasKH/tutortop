import { useState } from "react";
import { Browser } from "@wailsio/runtime";
import { Phase, UpdateService, errorText, type State } from "@/lib/api";
import { applyUpdate, downloadPercent, restartToUpdate, useUpdate } from "@/lib/update";
import { ErrorText } from "@/components/error-text";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";

/**
 * In-app update check, download, verification and restart. Downloads run in
 * the backend and continue if Settings is closed.
 */
export function UpdatesSetting() {
  const update = useUpdate();
  const [callError, setCallError] = useState("");

  if (!update) return <p className="text-muted-foreground">Loading…</p>;

  const phase = update.phase;
  const busyText = busyLabel(update);
  const pct = downloadPercent(update);
  const error = callError || (update.error ? errorText(update.error) : "");

  function run(work: () => Promise<unknown>) {
    setCallError("");
    work().catch((e) => setCallError(errorText(e)));
  }

  const check = () => run(() => UpdateService.Check().then(applyUpdate));
  const install = () => run(() => UpdateService.Install());
  const restart = () => run(restartToUpdate);
  const openRelease = () => run(() => Browser.OpenURL(update.releaseUrl));

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-sm text-muted-foreground">Tutortop</span>
      <div className="flex min-w-0 items-center justify-between gap-2 text-sm text-soft-foreground">
        <span className="min-w-0">Version {update.currentVersion}</span>
        <span className="flex flex-none gap-1">
          {(phase === Phase.PhaseIdle || phase === Phase.PhaseUpToDate) && (
            <Button type="button" variant="ghost" onClick={check}>
              Check for updates
            </Button>
          )}
          {phase === Phase.PhaseAvailable && update.canSelfUpdate && (
            <Button type="button" onClick={install}>
              {update.error ? "Retry download" : "Download and install"}
            </Button>
          )}
          {phase === Phase.PhaseAvailable && !update.canSelfUpdate && (
            <Button type="button" variant="ghost" onClick={openRelease}>
              Open releases
            </Button>
          )}
          {phase === Phase.PhaseReady && (
            <Button type="button" onClick={restart}>
              Restart to update
            </Button>
          )}
        </span>
      </div>

      {phase === Phase.PhaseUpToDate && !error && <p className="text-sm text-muted-foreground">You're on the latest version.</p>}

      {phase === Phase.PhaseAvailable && (
        <p className="text-sm text-soft-foreground">
          Tutortop {update.version} is available.
          {update.canSelfUpdate && (
            <>
              {" "}
              <Button type="button" variant="link" size="sm" className="h-auto px-0 text-sm" onClick={openRelease}>
                View on GitHub
              </Button>
            </>
          )}
        </p>
      )}

      {phase === Phase.PhaseReady && (
        <p className="text-sm text-soft-foreground">Tutortop {update.version} is downloaded and verified. Restart to finish updating.</p>
      )}

      {busyText && (
        <div className="flex flex-col gap-2">
          <p className="flex items-center gap-2 text-sm text-muted-foreground tabular-nums">
            <Spinner />
            {busyText}
          </p>
          {phase === Phase.PhaseDownloading && <Progress value={pct ?? 0} aria-valuetext={pct !== null ? `${pct}%` : "Downloading"} className="h-1 bg-input" />}
        </div>
      )}

      <ErrorText error={error} />

      {update.notes && phase !== Phase.PhaseIdle && phase !== Phase.PhaseUpToDate && (
        <div className="mt-1 rounded-lg border bg-card px-3.5 py-3">
          <p className="mb-1.5 text-xs font-medium text-muted-foreground">What's new in {update.version}</p>
          <Markdown text={update.notes} className="prose text-sm" />
        </div>
      )}

      <p className="text-xs text-muted-foreground">
        {update.canSelfUpdate
          ? "Updates are downloaded from GitHub Releases and verified with Tutortop's signing key before installing."
          : update.manualReason || "Download the latest version from GitHub Releases."}
      </p>
    </div>
  );
}

function busyLabel(update: State): string {
  switch (update.phase) {
    case Phase.PhaseChecking:
      return "Checking for updates…";
    case Phase.PhaseDownloading: {
      const pct = downloadPercent(update);
      if (pct === 100) return "Verifying and preparing update…";
      const prefix = update.version ? `Downloading Tutortop ${update.version}…` : "Downloading update…";
      const written = update.written ?? 0;
      const total = update.total ?? 0;
      if (total > 0 && pct !== null) return `${prefix} ${pct}% (${mb(written)} / ${mb(total)})`;
      if (written > 0) return `${prefix} ${mb(written)}`;
      return prefix;
    }
    case Phase.PhaseRestarting:
      return "Restarting Tutortop…";
    default:
      return "";
  }
}

function mb(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
