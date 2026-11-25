import { ArrowUpCircleIcon } from "lucide-react";
import { Phase, UpdateService } from "@/lib/api";
import { updateNoticeLabel, useUpdate } from "@/lib/update";
import { useSettings } from "@/features/settings/use-settings";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * Subtle button shown in the Home header or workspace sidebar when an update
 * is available, downloading or ready to restart. Renders nothing otherwise.
 */
export function UpdateNotice({ layout }: { layout: "header" | "sidebar" }) {
  const update = useUpdate();
  const { openSettings } = useSettings();
  const label = updateNoticeLabel(update);
  if (!update || !label) return null;

  const ready = update.phase === Phase.PhaseReady;
  const onClick = () => {
    if (ready) {
      UpdateService.Restart().catch(() => openSettings("updates"));
      return;
    }
    openSettings("updates");
  };

  if (layout === "header") {
    return (
      <Button type="button" variant={ready ? "default" : "ghost"} className="h-[35px] px-3 text-sm tabular-nums" onClick={onClick}>
        <ArrowUpCircleIcon size={15} /> {label}
      </Button>
    );
  }

  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-xs font-medium tabular-nums transition-colors",
        ready ? "bg-primary text-primary-foreground hover:bg-primary/90" : "text-soft-foreground hover:bg-muted hover:text-foreground",
      )}
      onClick={onClick}
    >
      <ArrowUpCircleIcon size={14} className="flex-none" />
      <span className="min-w-0 truncate">{label}</span>
    </button>
  );
}
