import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ChatRole, ChatService, ChatStatus, errorText, type ChatItem, type SandboxCheck } from "@/lib/api";
import { useTutor } from "@/features/settings/use-tutor";
import { TutorPicker } from "@/features/settings/tutor-picker";
import { AgentIcon } from "@/components/agent-icon";
import type { OpenItem } from "@/features/workspace/open-item";
import type { NewItem } from "@/features/workspace/use-open-item";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import type { Composer } from "@/features/chat/use-composer";
import { Thinking } from "@/features/chat/thinking";
import { MessageBox } from "@/features/chat/message-box";
import { Markdown } from "@/components/markdown";
import { IconButton } from "@/components/icon-button";
import { useArmed } from "@/hooks/use-armed";
import { ChevronDown, PanelRightIcon } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** How a conversation that isn't shown asks for attention: new or running work, and pending questions. */
export type Attention = { attention: boolean; questions: number };

/** What the visible conversation is about. It follows the centre pane: there are no tabs. */
type ChatContext = "Exercise" | "Lesson" | "Plan";

type Props = {
  /** The visible thread: "plan", a task id, or a lesson's thread (lib/workspace.ts). */
  thread: string;
  context: ChatContext;
  /** The open exercise's next hint level (1-4), or null when hints don't apply. */
  hint: { level: number; onHint: (level: number) => void } | null;
  items: ChatItem[];
  /** The thread's tutor is working (isBusy, workspace-state.ts): "Thinking…" shows and hints wait. */
  busy: boolean;
  /** Slots: the questions form (or a pointer to it) and the status line above the composer. */
  questions: ReactNode;
  status: ReactNode;
  /** The message box's draft and images, kept above the panel (use-composer.ts). */
  composer: Composer;
  /** Sends the learner's message; images are base64, as ChatService.SendMessage takes them. */
  onSend: (text: string, images: string[]) => void;
  onCollapse: () => void;
};

export function ChatPanel({ composer, ...p }: Props) {
  const tutor = useTutor();
  const listRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);

  const followBottom = useCallback(() => {
    const el = listRef.current;
    if (el && stickToBottom.current) el.scrollTop = el.scrollHeight;
  }, []);

  // A different thread starts at its latest message.
  useLayoutEffect(() => {
    stickToBottom.current = true;
  }, [p.thread]);

  // Follow new content unless the learner scrolled up to read.
  useLayoutEffect(followBottom);

  function onScroll() {
    const el = listRef.current!;
    stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
  }

  function send(text: string, images: string[]) {
    stickToBottom.current = true;
    p.onSend(text, images);
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
            <Message key={item.id} item={item} />
          ))}
          {p.questions}
          <Thinking thread={p.thread} items={p.items} busy={p.busy} onUpdate={followBottom} />
        </div>
        {p.hint && <HintPill level={p.hint.level} busy={p.busy} onHint={p.hint.onHint} />}
      </div>

      {p.status}
      <MessageBox composer={composer} placeholder={p.context === "Plan" ? "Ask about your plan" : `Ask about this ${p.context.toLowerCase()}`} onSend={send} />
      <TutorPicker side="top">
        <Button
          variant="ghost"
          className="mx-3 mb-2 h-auto max-w-[calc(100%-24px)] flex-none justify-start gap-1.5 self-start rounded-sm px-1 py-0.5 text-xs font-normal text-faint hover:not-disabled:bg-transparent hover:not-disabled:text-foreground aria-expanded:text-foreground"
          title="Change the tutor's agent, model and reasoning"
        >
          {tutor.agent && <AgentIcon id={tutor.agent} size={12} />}
          <span className="min-w-0 truncate">{tutor.label}</span>
          <ChevronDown size={12} aria-hidden="true" />
        </Button>
      </TutorPicker>
    </aside>
  );
}

/** The tutor folded into a strip: a way back, and a sign of new or running work. */
export function ChatRail({
  attention,
  busy,
  onExpand,
}: {
  attention: boolean;
  /** Some chat's tutor is working (tutorBusy). */
  busy: boolean;
  onExpand: () => void;
}) {
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
  /** The sandbox check of a language a chat is setting up (set_toolchain). */
  check: SandboxCheck | null;
  newItem: NewItem | null;
  /** The shown thread's tutor is working (isBusy, workspace-state.ts), so it can be stopped. */
  busy: boolean;
  onStop: () => void;
  onOpen: (item: OpenItem) => void;
};

/** One quiet line above the composer: a language being set up, new work, or the tutor working. */
export function StatusLine({ check, newItem, busy, onStop, onOpen }: StatusProps) {
  let text = "";
  let action: ReactNode = null;
  if (check?.running) {
    text = `Setting up ${check.title} in the sandbox. The first time can take a few minutes…`;
  } else if (newItem) {
    text = `${newItem.item.kind === "lesson" ? "New lesson" : "New exercise"}: ${newItem.title}`;
    action = (
      <Button variant="ghost" className="h-auto px-1.5 py-[3px] text-xs leading-normal" onClick={() => onOpen(newItem.item)}>
        Open
      </Button>
    );
  } else if (check && !check.passed) {
    // Normal: the tutor reads the check's report and calls set_toolchain again.
    text = `Fixing the ${check.title} setup…`;
  } else if (check?.passed) {
    text = `${check.title} is set up`;
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

function Message({ item }: { item: ChatItem }) {
  // Code in replies is coloured as the open item's language.
  const { lang } = useLiveWorkspace();
  switch (item.role) {
    case ChatRole.RoleUser:
      return (
        <div className="flex max-w-[85%] flex-col items-end gap-1.5 self-end">
          {item.images && item.images.length > 0 && (
            <div className="flex flex-wrap justify-end gap-1.5">
              {item.images.map((name) => (
                <ChatImage key={name} thread={item.thread} name={name} />
              ))}
            </div>
          )}
          {item.text && <div className="rounded-xl bg-muted px-[11px] py-2 leading-[1.5] wrap-anywhere whitespace-pre-wrap">{item.text}</div>}
        </div>
      );
    case ChatRole.RoleAssistant:
      return (
        <Markdown
          text={item.text}
          className="prose text-[13px] [&_:is(h1,h2,h3)]:mt-3 [&_:is(h1,h2,h3)]:font-sans [&_:is(h1,h2,h3)]:text-[13px] [&_:is(h1,h2,h3)]:leading-normal [&_:is(h1,h2,h3)]:font-semibold [&>*+*]:mt-2"
          lang={lang}
        />
      );
    case ChatRole.RoleTool:
      return (
        <div className="-my-2 flex items-center gap-1.5 text-xs text-muted-foreground" title={item.text}>
          <span className="grid w-3 flex-none">{item.status === ChatStatus.ChatRunning ? <Spinner className="size-[10px]" /> : "✓"}</span>
          <span className="min-w-0 truncate">{item.text}</span>
        </div>
      );
    case ChatRole.RoleNotice:
    case ChatRole.$zero:
      return (
        <div
          className="max-w-[88%] self-center text-center text-xs text-balance wrap-anywhere whitespace-pre-wrap text-muted-foreground"
          role={item.status === ChatStatus.ChatFailed ? "alert" : undefined}
        >
          {item.text.replace(/^\[app\]\s*/, "")}
        </div>
      );
  }
}

/** An image the learner attached to a message, loaded from where the app keeps it with the thread. */
function ChatImage({ thread, name }: { thread: string; name: string }) {
  const { id } = useLiveWorkspace();
  const [url, setUrl] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let current = true;
    ChatService.ChatImage(id, thread, name).then(
      (u) => current && setUrl(u),
      (e: unknown) => current && setError(errorText(e)),
    );
    return () => {
      current = false;
    };
  }, [id, thread, name]);
  if (error) return <div className="rounded-md border px-2 py-1 text-xs text-muted-foreground">Image unavailable</div>;
  if (!url) return <div className="size-24 rounded-md border bg-muted" aria-busy="true" />;
  return <img src={url} alt="Attached image" className="max-h-40 max-w-full rounded-md border object-contain" />;
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
