import { useEffect, useRef, useState, type FormEvent } from "react";
import { cn } from "cn";
import { ActivityPanel } from "@/features/home/activity-panel";
import { App, errorText, type Activity, type Environment, type Summary } from "@/lib/api";
import { IDEAS, type Idea } from "@/features/home/ideas";
import { SettingsIcon } from "lucide-react";
import { Logo } from "@/components/logo";
import { TutorLine } from "@/features/settings/settings-dialog";
import { TopicIcon } from "@/components/topic-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorText } from "@/components/error-text";
import { ProgressBar } from "@/components/progress-bar";
import { usePending } from "@/hooks/use-pending";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

type Props = {
  /** The tutor as configured in Settings, e.g. "Claude Code · Opus · High". */
  tutor: string;
  /** The workspace opened last, shown in focus. */
  lastId: string | null;
  onSettings: () => void;
  onOpen: (id: string) => void;
};

/** The topic in the new-workspace form: typed, or picked from an idea. */
type Draft = { title: string; icon: string };

export function Home({ tutor, lastId, onSettings, onOpen }: Props) {
  const [env, setEnv] = useState<Environment | null>(lastEnv);
  const [workspaces, setWorkspaces] = useState<Summary[] | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<Draft>({ title: "", icon: "" });
  const [activity, setActivity] = useState<Activity | null>(null);
  const [error, setError] = useState("");
  const mainRef = useRef<HTMLElement>(null);

  const list = workspaces ?? [];
  // One workspace is in focus: the last one opened, else the first.
  const focusId = list.some((w) => w.id === lastId) ? lastId : list[0]?.id;

  function startFrom(idea: Idea) {
    setDraft({ title: idea.title, icon: idea.icon });
    setCreating(true);
    mainRef.current?.scrollTo({ top: 0, behavior: "smooth" });
  }

  useEffect(() => {
    App.ListWorkspaces()
      .then((list) => {
        setWorkspaces(list ?? []);
        setCreating((list ?? []).length === 0);
      })
      .catch((e) => setError(errorText(e)));
    App.Activity()
      .then(setActivity)
      .catch(() => {});
    App.Environment()
      .then((e) => setEnv((lastEnv = e)))
      .catch((e) => setError(errorText(e)));
  }, []);

  return (
    <div className="flex h-full flex-col">
      {/* Name on the left, Settings on the right; no sidebar, the cards list the workspaces. */}
      <header className="flex flex-none items-center justify-between gap-4 border-b px-6 py-3.5">
        <div className="flex items-center gap-2.5">
          <Logo size={20} className="flex-none text-foreground" />
          <span className="text-[17px] leading-none font-bold">Tutortop</span>
        </div>
        <Button type="button" variant="outline" className="h-[35px] px-3.5 text-base [&_svg]:text-soft-foreground" onClick={onSettings}>
          <SettingsIcon size={16} /> Settings
        </Button>
      </header>

      <main ref={mainRef} className="min-h-0 flex-1 overflow-auto px-12 pt-9 pb-12">
        {/* One readable column, centred: wide screens get margins, not stretched cards. */}
        <div className="mx-auto max-w-[1180px]">
          <header className="mb-[26px] flex items-center justify-between gap-4">
            <h1 className="text-[30px] leading-[1.15] font-bold">Workspaces</h1>
            {!creating && (
              <Button
                onClick={() => {
                  setDraft({ title: "", icon: "" });
                  setCreating(true);
                }}
              >
                + New workspace
              </Button>
            )}
          </header>

          {env && !env.sandboxOk && (
            <p className="mb-5 text-sm text-soft-foreground" role="alert">
              Checks need Docker or Podman, and neither is available.{" "}
              {env.sandboxHint && <code className="mt-1.5 block whitespace-pre-wrap text-foreground">{env.sandboxHint}</code>}
            </p>
          )}
          <ErrorText error={error} />

          {creating && (
            <NewWorkspace
              key={draft.title}
              draft={draft}
              env={env}
              tutor={tutor}
              onSettings={onSettings}
              onCancel={() => setCreating(false)}
              onCreated={onOpen}
            />
          )}

          {activity && (
            <div className={cn(creating && DIMMED)}>
              <ActivityPanel activity={activity} />
            </div>
          )}

          {list.length > 0 && (
            // While a new workspace is being set up, the existing ones step back.
            <div className={cn(CARDS, creating && DIMMED)}>
              {list.map((w) => (
                <WorkspaceCard
                  key={w.id}
                  ws={w}
                  focused={w.id === focusId && list.length > 1}
                  onOpen={() => onOpen(w.id)}
                  onDelete={async () => {
                    await App.DeleteWorkspace(w.id);
                    setWorkspaces((cur) => (cur ?? []).filter((x) => x.id !== w.id));
                  }}
                />
              ))}
            </div>
          )}

          {workspaces && (
            <section className={cn("mt-12", !creating && DIMMED)} aria-labelledby="ideas-title">
              <header className="mb-4 flex items-baseline gap-3.5">
                <h2 id="ideas-title" className="text-[21px] leading-[1.2] font-bold">
                  Ideas
                </h2>
                <p className="text-sm text-muted-foreground">Start from one. Your tutor still plans it with you.</p>
              </header>
              <div className={CARDS}>
                {IDEAS.map((idea) => (
                  <IdeaCard key={idea.title} idea={idea} picked={creating && draft.title === idea.title} onPick={() => startFrom(idea)} />
                ))}
              </div>
            </section>
          )}
        </div>
      </main>
    </div>
  );
}

// Environment() checks the sandbox and can take a few seconds, so the last
// answer is kept while a fresh one loads.
let lastEnv: Environment | null = null;

// What's not in focus steps back until it's pointed at.
const DIMMED = "opacity-[0.42] transition-opacity duration-160 hover:opacity-100 focus-within:opacity-100";

const CARDS = "grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3.5";

// A card's frame. Focus marks the workspace you were last in, or the idea you picked.
const card = (focused: boolean) =>
  cn(
    "group relative flex flex-col rounded-xl border bg-card text-left transition-colors",
    focused ? "border-soft-foreground" : "border-input hover:border-faint",
  );

const CARD_BODY = "flex flex-1 flex-col gap-2 min-h-[150px] px-5 pt-[18px] pb-4 text-left";

const CARD_HEAD = "flex items-start justify-between gap-4";

const CARD_NAME = "text-[18px] leading-[1.3] font-[650] [overflow-wrap:anywhere]";

const NUM = "font-mono text-xs text-muted-foreground tabular-nums";

/** A workspace card. Delete asks first in a popup, since it removes the code and notes for good. */
function WorkspaceCard({ ws, focused, onOpen, onDelete }: { ws: Summary; focused: boolean; onOpen: () => void; onDelete: () => Promise<void> }) {
  const [confirming, setConfirming] = useState(false);
  const { passed, total } = ws.progress;
  return (
    <div className={card(focused)}>
      <button className={CARD_BODY} onClick={onOpen}>
        <span className={CARD_HEAD}>
          <span className={CARD_NAME}>{ws.title}</span>
          <TopicIcon name={ws.icon} size={30} />
        </span>
        <span className="truncate text-sm text-muted-foreground">
          {ws.next ? `Next: ${ws.next}` : total === 0 ? "No plan yet" : passed === total ? "All objectives passed" : ""}
        </span>
        <span className="mt-auto flex flex-col gap-2 pt-2.5">
          <ProgressBar passed={passed} total={total} />
          <span className={NUM}>
            {passed} / {total}
          </span>
        </span>
      </button>
      {/* shown on hover or focus */}
      <Button
        variant="ghost"
        className="absolute right-2.5 bottom-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        onClick={() => setConfirming(true)}
        title="Delete this workspace"
      >
        Delete
      </Button>
      {confirming && (
        <ConfirmDialog
          title={`Delete “${ws.title}”?`}
          confirmLabel="Delete workspace"
          pendingLabel="Deleting…"
          onConfirm={onDelete}
          onCancel={() => setConfirming(false)}
        >
          <p>This removes everything in it for good: your code, mission, notes, lessons, learning records, chats and progress.</p>
          <p className="text-muted-foreground">
            {passed} of {total} objectives passed. This can't be undone.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

function IdeaCard({ idea, picked, onPick }: { idea: Idea; picked: boolean; onPick: () => void }) {
  return (
    <button className={cn(card(picked), CARD_BODY)} onClick={onPick} aria-pressed={picked}>
      <span className={CARD_HEAD}>
        <span className={CARD_NAME}>{idea.title}</span>
        <TopicIcon name={idea.icon} size={26} />
      </span>
      <span className="overflow-hidden text-sm text-ellipsis text-muted-foreground">{idea.blurb}</span>
      <span className="mt-auto flex items-center justify-between gap-2 pt-2.5 text-muted-foreground group-hover:text-foreground">
        <span className={NUM}>{idea.language}</span>
        <span aria-hidden="true">→</span>
      </span>
    </button>
  );
}

function NewWorkspace({
  draft,
  env,
  tutor,
  onSettings,
  onCancel,
  onCreated,
}: {
  draft: Draft;
  env: Environment | null;
  tutor: string;
  onSettings: () => void;
  onCancel: () => void;
  onCreated: (id: string) => void;
}) {
  const [title, setTitle] = useState(draft.title);
  const { pending, error, setError, run } = usePending();
  const providers = env?.providers ?? [];
  const noAgent = !!env && !providers.some((p) => p.installed);
  // "Claude Code or Codex": every agent the backend offers.
  const agents = new Intl.ListFormat("en", { type: "disjunction" }).format(providers.map((p) => p.name));

  function submit(e: FormEvent) {
    e.preventDefault();
    // An idea's icon stays only while its title does; otherwise the tutor picks one.
    const icon = title.trim() === draft.title ? draft.icon : "";
    if (title.trim() && !noAgent) run(async () => onCreated(await App.CreateWorkspace(title.trim(), icon)));
  }

  return (
    <form className="mb-7 flex max-w-[480px] flex-col gap-[18px] rounded-xl border bg-card px-5 py-[18px]" onSubmit={submit}>
      <label className={FIELD}>
        <span className={FIELD_LABEL}>Topic</span>
        <Input
          value={title}
          aria-invalid={!!error}
          aria-describedby={error ? "new-ws-error" : undefined}
          onChange={(e) => {
            setTitle(e.target.value);
            setError("");
          }}
          placeholder="Go with htmx"
          autoFocus
        />
        {error && (
          <span id="new-ws-error" className="text-sm whitespace-pre-wrap text-fail" role="alert">
            {error}
          </span>
        )}
      </label>

      <div className={FIELD}>
        <span className={FIELD_LABEL}>Tutor</span>
        <TutorLine tutor={tutor} onSettings={onSettings} />
        <ErrorText error={noAgent ? `Install ${agents}, sign in, then restart Tutortop.` : ""} />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button disabled={!title.trim() || noAgent || pending}>{pending ? "Creating…" : "Create"}</Button>
        <Button type="button" variant="ghost" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

const FIELD = "flex min-w-0 flex-col gap-1.5";

const FIELD_LABEL = "text-sm text-muted-foreground";
