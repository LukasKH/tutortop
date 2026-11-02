import { useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";
import { type ChatItem, type GateEvent } from "./api";
import type { NewItem } from "./WorkspaceView";
import { Markdown } from "./Markdown";
import { AutoTextarea, IconButton, useArmed } from "./ui";
import { PanelRight, Spinner } from "./icons";

/** How a conversation that isn't shown asks for attention: new or running work, and pending questions. */
export type Attention = { attention: boolean; questions: number };

/** What the visible conversation is about. It follows the centre pane: there are no tabs. */
export type ChatContext = "Exercise" | "Lesson" | "Plan";

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

export function ChatPanel(p: Props) {
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
    <aside className="tutor">
      <header className="tutor-head">
        <h2 className="tutor-title">Tutor</h2>
        <span className="tutor-context">{p.context}</span>
        <IconButton label="Hide tutor" onClick={p.onCollapse} aria-expanded>
          <PanelRight />
        </IconButton>
      </header>

      <div className="thread-wrap">
      <div className={`thread ${p.hint ? "has-pill" : ""}`} ref={listRef} onScroll={onScroll}>
        {p.items.length === 0 && !p.busy && !p.questions && (
          <p className="thread-empty">
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
          <div className="msg-working" role="status">
            <Spinner size={12} /> Thinking…
          </div>
        )}
      </div>
      {p.hint && <HintPill level={p.hint.level} busy={p.busy} onHint={p.hint.onHint} />}
      </div>

      {p.status}
      <div className="composer">
        <AutoTextarea
          ref={p.inputRef}
          className="composer-input"
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
        <button className="send" onClick={send} disabled={!p.draft.trim()} aria-label="Send" title="Send (Enter)">
          ↑
        </button>
      </div>
      <button className="composer-tutor" onClick={p.onSettings} title="Change the tutor's agent, model and reasoning">
        <span className="truncate">{p.tutor}</span>
      </button>
    </aside>
  );
}

const HINT_LEVELS = ["", "a nudge", "a pointer", "a snippet", "the solution"];

/**
 * Hints float over the conversation they answer in. Each press goes one
 * level deeper (nudge → pointer → snippet → solution); the solution asks first.
 */
function HintPill({ level, busy, onHint }: { level: number; busy: boolean; onHint: (level: number) => void }) {
  const [armed, arm] = useArmed();
  return (
    <button
      className={`hint-pill ${armed ? "is-armed" : ""}`}
      disabled={busy}
      title={busy ? "Wait for the tutor to finish" : `Ask for ${HINT_LEVELS[level]}`}
      onClick={() => (level === 4 && !armed ? arm() : onHint(level))}
    >
      {armed ? "Show the solution?" : level === 1 ? "Hint" : level === 4 ? "Show solution" : "Another hint"}
    </button>
  );
}

/** The tutor folded into a strip: a way back, and a sign of new or running work. */
export function ChatRail({ attention, busy, onExpand }: { attention: boolean; busy: boolean; onExpand: () => void }) {
  return (
    <aside className="tutor-rail">
      <IconButton label="Show tutor" onClick={onExpand} aria-expanded={false}>
        <PanelRight />
      </IconButton>
      <button className={`rail-label ${attention ? "is-loud" : ""}`} onClick={onExpand} title={busy ? "Tutor is working" : "Show tutor"}>
        Tutor
      </button>
      {busy && <Spinner size={12} />}
    </aside>
  );
}

function Message({ item, lang }: { item: ChatItem; lang?: string }) {
  switch (item.role) {
    case "user":
      return <div className="msg-me">{item.text}</div>;
    case "assistant":
      return <Markdown text={item.text} className="prose msg-ai" lang={lang} />;
    case "tool":
      return (
        <div className={`msg-tool ${item.status === "failed" ? "fail" : ""}`} title={item.text}>
          <span className="msg-tool-mark">{item.status === "running" ? <Spinner size={10} /> : item.status === "failed" ? "✗" : "✓"}</span>
          <span className="truncate">{item.text}</span>
        </div>
      );
    default:
      return (
        <div className={`msg-notice ${item.status === "failed" ? "fail" : ""}`} role={item.status === "failed" ? "alert" : undefined}>
          {item.text.replace(/^\[app\]\s*/, "")}
        </div>
      );
  }
}

type StatusProps = {
  gate: GateEvent | null;
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
      <button className="btn text" onClick={() => onOpenTask(newTask.id)}>
        Open
      </button>
    );
  } else if (gate && !gate.passed) {
    // Normal: the tutor (in the plan thread) reads the gate report and resubmits.
    text = setup ? `Fixing the ${gate.title} setup…` : `Fixing “${gate.title}” after the sandbox checks…`;
  } else if (gate?.passed) {
    text = setup ? `${gate.title} is set up` : `“${gate.title}” is ready`;
  }
  if (!text && !busy) return null;
  return (
    <div className="status-line" title={text}>
      <span className="status-text">{text}</span>
      {action}
      {busy && (
        <button className="btn text" onClick={onStop} title="Stop the tutor's current turn">
          Stop
        </button>
      )}
    </div>
  );
}
