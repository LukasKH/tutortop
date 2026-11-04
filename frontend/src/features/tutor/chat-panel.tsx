import { useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";
import { type ChatItem, type Gate } from "@/lib/api";
import type { NewItem } from "@/lib/workspace";
import { Markdown } from "@/components/markdown";
import { AutoTextarea } from "@/components/auto-textarea";
import { IconButton } from "@/components/icon-button";
import { useArmed } from "@/hooks/use-armed";
import { PanelRightIcon } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Button } from "@/components/ui/button";
import { cn } from "cn";

/** How a conversation that isn't shown asks for attention: new or running work, and pending questions. */
export type Attention = { attention: boolean; questions: number };

/** What the visible conversation is about. It follows the centre pane: there are no tabs. */
type ChatContext = "Exercise" | "Lesson" | "Plan";

type Props = {
  /** The visible thread id: "plan", a task id, or a lesson's key. */
  thread: string;
  context: ChatContext;
  /** The language of the open exercise or plan, for colouring code in replies. */
  lang?: string;
  /** The open exercise's next hint level (1-4), or null when hints don't apply. */
  hint: { level: number; onHint: (level: number) => void } | null;
  items: ChatItem[];
  busy: boolean;
  /** Slots: the questions form (or a pointer to it) and the status line above the composer. */
  questions: ReactNode;
  status: ReactNode;
  draft: string;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  onDraft: (text: string) => void;
  onSend: (text: string) => void;
  onCollapse: () => void;
  /** The configured agent, model and effort, shown under the composer; clicking opens Settings. */
  tutor: string;
  onSettings: () => void;
};

export function ChatPanel({ inputRef, ...p }: Props) {
  const listRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);

  // A different thread starts at its latest message.
  useLayoutEffect(() => {
    stickToBottom.current = true;
  }, [p.thread]);

  // Follow new content unless the learner scrolled up to read.
  useLayoutEffect(() => {
    const el = listRef.current;
    if (el && stickToBottom.current) el.scrollTop = el.scrollHeight;
  });

  function onScroll() {
    const el = listRef.current!;
    stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
  }

  function send() {
    const text = p.draft.trim();
    if (!text) return;
    stickToBottom.current = true;
    p.onDraft("");
    p.onSend(text);
  }

  return (
    <aside className="flex min-h-0 min-w-0 flex-col border-l bg-card">
      <header className="flex h-[50px] flex-none items-stretch gap-4 border-b pr-2 pl-4">
        <h2 className="mr-auto self-center font-sans text-[15px] leading-none font-[650]">Tutor</h2>
        <span className="self-center text-sm text-muted-foreground">{p.context}</span>
        <IconButton label="Hide tutor" className="self-center" onClick={p.onCollapse} aria-expanded>
          <PanelRightIcon size={16} />
        </IconButton>
      </header>

      <div className="relative flex min-h-0 flex-1 flex-col">
        <div className={cn("flex flex-1 flex-col gap-3.5 overflow-y-auto p-4 text-[13px]", p.hint && "pb-14")} ref={listRef} onScroll={onScroll}>
          {p.items.length === 0 && !p.busy && !p.questions && (
            <p className="my-auto px-3 text-center text-sm text-muted-foreground">
              {p.context === "Plan"
                ? "Your mission and plan. Ask for changes any time."
                : `Ask about this ${p.context.toLowerCase()}. Each one starts a new conversation.`}
            </p>
          )}
          {p.items.map((item) => (
            <Message key={item.id} item={item} lang={p.lang} />
          ))}
          {p.questions}
          {p.busy && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
              <Spinner className="size-[12px]" aria-hidden /> Thinking…
            </div>
          )}
        </div>
        {p.hint && <HintPill level={p.hint.level} busy={p.busy} onHint={p.hint.onHint} />}
      </div>

      {p.status}
      <div className="mx-3 mb-1 flex flex-none items-end gap-2 rounded-xl border border-input py-2 pr-2 pl-3 transition-colors focus-within:border-foreground">
        <AutoTextarea
          ref={inputRef}
          className="flex-1 rounded-none border-0 bg-transparent px-0 py-1 text-[13px] leading-[1.5] focus-visible:border-0"
          maxHeight={180}
          value={p.draft}
          placeholder={p.context === "Plan" ? "Ask about your plan" : `Ask about this ${p.context.toLowerCase()}`}
          aria-label="Message the tutor"
          onChange={(e) => p.onDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              send();
            }
          }}
        />
        <Button
          size="icon-sm"
          className="font-normal hover:not-disabled:border-primary hover:not-disabled:bg-primary disabled:border-input disabled:bg-input disabled:opacity-100"
          onClick={send}
          disabled={!p.draft.trim()}
          aria-label="Send"
          title="Send (Enter)"
        >
          ↑
        </Button>
      </div>
      <Button
        variant="ghost"
        className="mx-3 mb-2 h-auto max-w-[calc(100%-24px)] flex-none justify-start rounded-sm px-1 py-0.5 text-xs font-normal text-faint hover:not-disabled:bg-transparent hover:not-disabled:text-foreground"
        onClick={p.onSettings}
        title="Change the tutor's agent, model and reasoning"
      >
        <span className="min-w-0 truncate">{p.tutor}</span>
      </Button>
    </aside>
  );
}

/** The tutor folded into a strip: a way back, and a sign of new or running work. */
export function ChatRail({ attention, busy, onExpand }: { attention: boolean; busy: boolean; onExpand: () => void }) {
  return (
    <aside className="flex flex-col items-center gap-2 border-l bg-card py-2.5">
      <IconButton label="Show tutor" onClick={onExpand} aria-expanded={false}>
        <PanelRightIcon size={16} />
      </IconButton>
      <Button
        variant="ghost"
        className={cn(
          "h-auto rounded-none p-0 text-xs font-normal text-muted-foreground [writing-mode:vertical-rl] hover:not-disabled:bg-transparent hover:not-disabled:text-foreground",
          attention && "font-semibold text-foreground",
        )}
        onClick={onExpand}
        title={busy ? "Tutor is working" : "Show tutor"}
      >
        Tutor
      </Button>
      {busy && <Spinner className="size-[12px] text-muted-foreground" />}
    </aside>
  );
}

type StatusProps = {
  gate: Gate | null;
  newTask: NewItem | null;
  busy: boolean;
  onStop: () => void;
  onOpenTask: (id: string) => void;
};

/** One quiet line above the composer: sandbox checks, a new task, or the tutor working. */
export function StatusLine({ gate, newTask, busy, onStop, onOpenTask }: StatusProps) {
  let text = "";
  let action: ReactNode = null;
  const setup = gate?.kind === "toolchain";
  if (gate?.running) {
    text = setup ? `Setting up ${gate.title} in the sandbox. The first time can take a few minutes…` : `Checking “${gate.title}” in the sandbox…`;
  } else if (newTask) {
    text = `${newTask.kind === "lesson" ? "New lesson" : "New exercise"}: ${newTask.title}`;
    action = (
      <Button variant="ghost" className="h-auto px-1.5 py-[3px] text-xs leading-normal" onClick={() => onOpenTask(newTask.id)}>
        Open
      </Button>
    );
  } else if (gate && !gate.passed) {
    // Normal: the tutor (in the plan thread) reads the gate report and resubmits.
    text = setup ? `Fixing the ${gate.title} setup…` : `Fixing “${gate.title}” after the sandbox checks…`;
  } else if (gate?.passed) {
    text = setup ? `${gate.title} is set up` : `“${gate.title}” is ready`;
  }
  if (!text && !busy) return null;
  return (
    <div className="flex flex-none items-center gap-2 pr-3 pb-1.5 pl-4 text-xs text-muted-foreground" title={text}>
      <span className="min-h-[1em] min-w-0 flex-1 wrap-anywhere">{text}</span>
      {action}
      {busy && (
        <Button variant="ghost" className="h-auto px-1.5 py-[3px] text-xs leading-normal" onClick={onStop} title="Stop the tutor's current turn">
          Stop
        </Button>
      )}
    </div>
  );
}

function Message({ item, lang }: { item: ChatItem; lang?: string }) {
  switch (item.role) {
    case "user":
      return <div className="max-w-[85%] self-end rounded-xl bg-muted px-[11px] py-2 leading-[1.5] wrap-anywhere whitespace-pre-wrap">{item.text}</div>;
    case "assistant":
      return (
        <Markdown
          text={item.text}
          className="prose text-[13px] [&_:is(h1,h2,h3)]:mt-3 [&_:is(h1,h2,h3)]:font-sans [&_:is(h1,h2,h3)]:text-[13px] [&_:is(h1,h2,h3)]:leading-normal [&_:is(h1,h2,h3)]:font-semibold [&>*+*]:mt-2"
          lang={lang}
        />
      );
    case "tool":
      return (
        <div className="-my-2 flex items-center gap-1.5 text-xs text-muted-foreground" title={item.text}>
          <span className="grid w-3 flex-none">{item.status === "running" ? <Spinner className="size-[10px]" /> : item.status === "failed" ? "✗" : "✓"}</span>
          <span className="min-w-0 truncate">{item.text}</span>
        </div>
      );
    default:
      return (
        <div
          className="max-w-[88%] self-center text-center text-xs text-balance wrap-anywhere whitespace-pre-wrap text-muted-foreground"
          role={item.status === "failed" ? "alert" : undefined}
        >
          {item.text.replace(/^\[app\]\s*/, "")}
        </div>
      );
  }
}

/**
 * Hints float over the conversation they answer in. Each press goes one
 * level deeper (nudge → pointer → snippet → solution); the solution asks first.
 */
function HintPill({ level, busy, onHint }: { level: number; busy: boolean; onHint: (level: number) => void }) {
  const [armed, arm] = useArmed();
  return (
    <Button
      variant="outline"
      className={cn(
        "absolute right-4 bottom-3 z-3 h-auto rounded-full bg-muted px-3.5 py-1.5 font-semibold shadow-[0_6px_20px_rgba(0,0,0,0.45)] hover:not-disabled:border-soft-foreground hover:not-disabled:bg-accent",
        armed && "border-fail text-fail",
      )}
      disabled={busy}
      title={busy ? "Wait for the tutor to finish" : `Ask for ${HINT_LEVELS[level]}`}
      onClick={() => (level === 4 && !armed ? arm() : onHint(level))}
    >
      {armed ? "Show the solution?" : level === 1 ? "Hint" : level === 4 ? "Show solution" : "Another hint"}
    </Button>
  );
}

const HINT_LEVELS = ["", "a nudge", "a pointer", "a snippet", "the solution"];
