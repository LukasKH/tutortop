import { ChevronDownIcon } from "lucide-react";
import type { ChatItem } from "@/lib/api";
import { showThinking, workingLine } from "@/lib/chat";
import { useWorking } from "@/features/workspace/use-working";
import { Markdown } from "@/components/markdown";
import { Spinner } from "@/components/ui/spinner";

type Props = {
  /** Thread whose live working state to subscribe to (useWorkingStore). */
  thread: string;
  items: ChatItem[];
  busy: boolean;
};

/**
 * Under a conversation while its tutor works and nothing else in it moves
 * (showThinking): the tool call the tutor is writing, or the latest line of
 * its reasoning, which unfolds in full. "Thinking…" when its agent says
 * neither.
 */
export function Thinking({ thread, items, busy }: Props) {
  const working = useWorking(thread);
  if (!showThinking(items, busy, working)) return null;
  const label = workingLine(working) ?? "Thinking…";
  const line = (
    <span className="flex min-w-0 items-center gap-2">
      <Spinner className="size-[12px] flex-none" aria-hidden />
      <span className="min-w-0 truncate">{label}</span>
    </span>
  );
  if (!working?.thinking.trim()) {
    return (
      <div className="flex min-w-0 text-sm text-muted-foreground" role="status">
        {line}
      </div>
    );
  }
  return (
    <details className="group min-w-0 text-sm text-muted-foreground">
      <summary className="flex w-fit max-w-full min-w-0 cursor-pointer list-none items-center gap-2 hover:text-soft-foreground [&::-webkit-details-marker]:hidden">
        {line}
        <ChevronDownIcon size={12} className="flex-none -rotate-90 transition-transform group-open:rotate-0" aria-hidden />
      </summary>
      <Markdown
        text={working.thinking}
        className="prose mt-2 border-l-2 border-input pl-3 text-xs text-muted-foreground [&_:is(h1,h2,h3)]:mt-2 [&_:is(h1,h2,h3)]:font-sans [&_:is(h1,h2,h3)]:text-xs [&_:is(h1,h2,h3)]:leading-normal [&_:is(h1,h2,h3)]:font-semibold [&_strong]:text-soft-foreground [&>*+*]:mt-1.5"
      />
    </details>
  );
}
