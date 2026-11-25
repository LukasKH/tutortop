import { DownloadIcon } from "lucide-react";
import { Phase } from "@/lib/api";
import { restartToUpdate, updateNoticeLabel, useUpdate } from "@/lib/update";
import { useSettings } from "@/features/settings/use-settings";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

/**
 * Subtle update control shown in the Home header (pill) or workspace sidebar
 * (circular download icon with a notification dot) when an update is
 * available, downloading or ready to restart. Renders nothing otherwise.
 */
export function UpdateNotice({ layout }: { layout: "header" | "sidebar" }) {
  const update = useUpdate();
  const { openSettings } = useSettings();
  const label = updateNoticeLabel(update);
  if (!update || !label) return null;

  const ready = update.phase === Phase.PhaseReady;
  const busy = update.phase === Phase.PhaseDownloading || update.phase === Phase.PhaseRestarting;
  const onClick = () => {
    if (ready) {
      restartToUpdate().catch(() => openSettings("updates"));
      return;
    }
    openSettings("updates");
  };

  if (layout === "header") {
    return (
      <Button type="button" variant={ready ? "default" : "ghost"} className="h-[35px] px-3 text-sm tabular-nums" onClick={onClick}>
        {busy ? <Spinner /> : <DownloadIcon size={15} />} {label}
      </Button>
    );
  }

  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className={cn(
        "relative flex size-8 items-center justify-center rounded-full transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
        ready ? "bg-primary text-primary-foreground hover:bg-primary/90" : "bg-muted text-foreground hover:bg-accent",
      )}
    >
      {busy ? <Spinner className="size-4" /> : <DownloadIcon size={15} />}
      {!busy && (
        <span className={cn("absolute top-1.5 right-1.5 size-1.5 rounded-full", ready ? "bg-primary-foreground" : "bg-foreground")} aria-hidden="true" />
      )}
    </button>
  );
}
