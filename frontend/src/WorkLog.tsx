import { useEffect, useState } from "react";
import { plural, type ChatItem, type GateEvent } from "./api";
import { ChevronDown, Spinner } from "./icons";
import { Markdown } from "./Markdown";

type Props = {
  /** One stretch of the planning conversation: what the tutor did since the learner last wrote. */
  items: ChatItem[];
  busy: boolean;
  gate?: GateEvent | null;
  lang?: string;
  onStop?: () => void;
};

/**
 * What the tutor is doing, for the centre of the page: a "Working for…"
 * line, its messages, and its tool calls folded into one summary line per
 * run (expand it to see each call).
 */
export function WorkLog({ items, busy, gate, lang, onStop }: Props) {
  const now = useNow(busy);
  // Before the first item arrives, the clock runs from when the work started.
  const [since, setSince] = useState(now);
  useEffect(() => {
    if (busy) setSince(Date.now());
  }, [busy]);
  const start = items[0] ? Date.parse(items[0].at) : since;
  const end = busy ? now : items.length ? Date.parse(items.at(-1)!.at) : start;
  const elapsed = formatDuration(Math.max(0, end - start));

  const groups = groupTools(items.filter((i) => i.role !== "user" || !i.text.startsWith("My answers:")));
  const last = groups.at(-1);
  // Something visibly moving already (a running tool or text streaming in) needs no extra "Thinking".
  const moving = Array.isArray(last) ? last.some((t) => t.status === "running") : last?.role === "assistant";

  return (
    <section className="worklog" aria-live="polite">
      <div className="worklog-head">
        {busy && <Spinner size={12} />}
        <span>{busy ? `Working for ${elapsed}` : `Worked for ${elapsed}`}</span>
        {busy && onStop && (
          <button className="btn text worklog-stop" onClick={onStop} title="Stop the tutor's current turn">
            Stop
          </button>
        )}
      </div>
      {groups.map((g, i) =>
        Array.isArray(g) ? (
          <ToolGroup key={g[0].id} tools={g} />
        ) : g.role === "assistant" ? (
          <Markdown key={g.id} text={g.text} className="prose worklog-text" lang={lang} />
        ) : g.role === "user" ? (
          <p key={g.id} className="worklog-you">
            {g.text}
          </p>
        ) : (
          <p key={g.id} className={`worklog-notice ${g.status === "failed" ? "fail" : ""}`} role={g.status === "failed" ? "alert" : undefined}>
            {g.text.replace(/^\[app\]\s*/, "")}
          </p>
        ),
      )}
      {busy && gate?.running && (
        <div className="worklog-step">
          <Spinner size={12} />
          {gate.kind === "toolchain" ? `Setting up ${gate.title} in the sandbox. The first time can take a few minutes…` : `Checking “${gate.title}” in the sandbox…`}
        </div>
      )}
      {busy && !moving && !gate?.running && (
        <div className="worklog-step">
          <Spinner size={12} /> Thinking…
        </div>
      )}
    </section>
  );
}

function ToolGroup({ tools }: { tools: ChatItem[] }) {
  const running = tools.find((t) => t.status === "running");
  const failed = tools.filter((t) => t.status === "failed").length;
  return (
    <details className="worklog-tools">
      <summary>
        <span className="worklog-mark">{running ? <Spinner size={11} /> : failed ? "✗" : "✓"}</span>
        <span className="truncate">{summarize(tools)}</span>
        <ChevronDown size={12} />
      </summary>
      <ul>
        {tools.map((t) => (
          <li key={t.id} className={t.status === "failed" ? "fail" : undefined} title={t.text}>
            <span className="worklog-mark">{t.status === "running" ? <Spinner size={10} /> : t.status === "failed" ? "✗" : "✓"}</span>
            <span className="truncate">{t.text}</span>
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
    if (item.role !== "tool") out.push(item);
    else if (Array.isArray(prev)) prev.push(item);
    else out.push([item]);
  }
  return out;
}

/** "Read 18 pages, updated your mission and noted your preferences". */
export function summarize(tools: ChatItem[]): string {
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
  const text = parts.length > 1 ? `${parts.slice(0, -1).join(", ")} and ${parts.at(-1)}` : (parts[0] ?? "");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return s % 60 ? `${m}m ${s % 60}s` : `${m}m`;
}

/** The current time, ticking every second while on. */
function useNow(on: boolean): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!on) return;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [on]);
  return now;
}
