import { useEffect, useState } from "react";
import { plural } from "@/lib/format";
import { ChatRole, ChatStatus, type ChatItem, type SandboxCheck, type Working } from "@/lib/api";
import { ChevronDownIcon } from "lucide-react";
import { Thinking } from "@/features/chat/thinking";
import { Spinner } from "@/components/ui/spinner";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type Props = {
  /** One stretch of the planning conversation: what the tutor did since the learner last wrote. */
  items: ChatItem[];
  busy: boolean;
  /** What the tutor is working on between messages: its reasoning, or a long tool call it's writing. */
  working?: Working;
  /** The tutor's sandbox check of a language it sets up. */
  check?: SandboxCheck | null;
  onStop?: () => void;
};

/**
 * What the tutor is doing, for the centre of the page: a "Working for…"
 * line, its messages, its tool calls folded into one summary line per run
 * (expand it to see each call), and what it's thinking or writing now.
 */
export function WorkLog({ items, busy, working, check, onStop }: Props) {
  // Before the first item arrives, the clock runs from when the work started.
  const { since, now } = useClock(busy);
  const start = items[0] ? Date.parse(items[0].at) : since;
  const end = busy ? now : items.length ? Date.parse(items.at(-1)!.at) : start;
  const elapsed = formatDuration(Math.max(0, end - start));

  const shown = items.filter((i) => i.role !== ChatRole.RoleUser || !i.text.startsWith("My answers:"));
  const groups = groupTools(shown);

  return (
    <section className="mt-7 flex flex-col gap-3" aria-live="polite">
      <div className="flex items-center gap-2 border-b pb-2.5 text-sm text-soft-foreground">
        {busy && <Spinner className="size-[12px]" />}
        <span>{busy ? `Working for ${elapsed}` : `Worked for ${elapsed}`}</span>
        {busy && onStop && (
          <Button variant="ghost" className="ml-auto" onClick={onStop} title="Stop the tutor's current turn">
            Stop
          </Button>
        )}
      </div>
      {groups.map((g) => (Array.isArray(g) ? <ToolGroup key={g[0].id} tools={g} /> : <Entry key={g.id} item={g} />))}
      {busy && check?.running && (
        <div className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground">
          <Spinner className="size-[12px]" aria-hidden />
          Setting up {check.title} in the sandbox. The first time can take a few minutes…
        </div>
      )}
      {!check?.running && <Thinking items={shown} busy={busy} working={working} />}
    </section>
  );
}

/** One item of the log that isn't a tool call. */
function Entry({ item }: { item: ChatItem }) {
  switch (item.role) {
    case ChatRole.RoleAssistant:
      return <Markdown text={item.text} className="prose" />;
    case ChatRole.RoleUser:
      return <p className="border-l-2 border-input pl-3 wrap-anywhere whitespace-pre-wrap text-soft-foreground">{item.text}</p>;
    case ChatRole.RoleTool:
      return <ToolGroup tools={[item]} />;
    case ChatRole.RoleNotice:
    case ChatRole.$zero:
      return (
        <p
          className={cn("text-sm text-muted-foreground", item.status === ChatStatus.ChatFailed && "text-fail")}
          role={item.status === ChatStatus.ChatFailed ? "alert" : undefined}
        >
          {item.text.replace(/^\[app\]\s*/, "")}
        </p>
      );
  }
}

/** Failed calls never get here: upsertChat and mergeChat drop them. */
function ToolGroup({ tools }: { tools: ChatItem[] }) {
  const running = tools.find((t) => t.status === ChatStatus.ChatRunning);
  return (
    <details className="group">
      <summary className="flex w-fit max-w-full min-w-0 cursor-pointer list-none items-center gap-2 text-sm text-muted-foreground hover:text-soft-foreground [&::-webkit-details-marker]:hidden">
        <span className="grid w-3 flex-none place-items-center">{running ? <Spinner className="size-[11px]" /> : "✓"}</span>
        <span className="min-w-0 truncate">{summarize(tools)}</span>
        <ChevronDownIcon size={12} className="flex-none -rotate-90 transition-transform group-open:rotate-0" />
      </summary>
      <ul className="mt-2 flex flex-col gap-0.5 pl-5">
        {tools.map((t) => (
          <li key={t.id} className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground" title={t.text}>
            <span className="grid w-3 flex-none place-items-center">{t.status === ChatStatus.ChatRunning ? <Spinner className="size-[10px]" /> : "✓"}</span>
            <span className="min-w-0 truncate">{t.text}</span>
          </li>
        ))}
      </ul>
    </details>
  );
}

/** Consecutive tool calls form one group; messages stand alone. */
function groupTools(items: ChatItem[]): (ChatItem | ChatItem[])[] {
  const out: (ChatItem | ChatItem[])[] = [];
  for (const item of items) {
    const prev = out.at(-1);
    if (item.role !== ChatRole.RoleTool) out.push(item);
    else if (Array.isArray(prev)) prev.push(item);
    else out.push([item]);
  }
  return out;
}

/** "Read 18 pages, updated your mission, and noted your preferences". */
function summarize(tools: ChatItem[]): string {
  let pages = 0;
  let searches = 0;
  const other: string[] = [];
  for (const t of tools) {
    if (t.text.startsWith("Fetched ")) pages++;
    else if (t.text.startsWith("Searched the web")) searches++;
    else {
      const phrase = t.text.charAt(0).toLowerCase() + t.text.slice(1);
      if (!other.includes(phrase)) other.push(phrase);
    }
  }
  const parts = [
    ...(searches ? [`searched the web ${searches === 1 ? "once" : `${searches} times`}`] : []),
    ...(pages ? [`read ${plural(pages, "page")}`] : []),
    ...other,
  ];
  const text = new Intl.ListFormat("en-GB").format(parts);
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return s % 60 ? `${m}m ${s % 60}s` : `${m}m`;
}

/** The time while on, ticking every second, and when it was last turned on. */
function useClock(on: boolean): { since: number; now: number } {
  const [clock, setClock] = useState(() => ({ since: Date.now(), now: Date.now() }));
  useEffect(() => {
    if (!on) return;
    const since = Date.now();
    const tick = () => setClock({ since, now: Date.now() });
    const first = setTimeout(tick);
    const every = setInterval(tick, 1000);
    return () => {
      clearTimeout(first);
      clearInterval(every);
    };
  }, [on]);
  return clock;
}
