import type { ChatItem } from "@/lib/api";
import { showThinking } from "@/lib/chat";
import { Spinner } from "@/components/ui/spinner";

/** "Thinking…" under a conversation while its tutor works and nothing else in it moves (showThinking). */
export function Thinking({ items, busy }: { items: ChatItem[]; busy: boolean }) {
  if (!showThinking(items, busy)) return null;
  return (
    <div className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground" role="status">
      <Spinner className="size-[12px]" aria-hidden /> Thinking…
    </div>
  );
}
