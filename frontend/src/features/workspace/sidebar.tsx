import type { ReactNode } from "react";
import { lessonDone } from "@/lib/workspace";
import { type LessonStatus, type TaskStatus, type WorkspaceSnapshot } from "@/lib/api";
import { describeItem, entryOf, itemKey, lessonOf, sameItem, type OpenItem } from "@/features/workspace/open-item";
import type { Attention } from "@/features/chat/chat-panel";
import { PencilIcon } from "lucide-react";
import { cn } from "cn";
import { Spinner } from "@/components/ui/spinner";
import { TopicIcon } from "@/components/topic-icon";
import { ProgressBar } from "@/components/progress-bar";

type Props = {
  ws: WorkspaceSnapshot;
  /** The open item, or null while a plan page is open. */
  openItem: OpenItem | null;
  planOpen: boolean;
  /** The planning tutor wants attention: new messages, work, or questions waiting. */
  plan: Attention;
  /** Objectives whose lesson and exercise are being written in the background (writingObjectives, workspace-state.ts). */
  writing: Set<string>;
  onBack: () => void;
  onOpen: (item: OpenItem) => void;
  onShowPlan: () => void;
};

/**
 * The curriculum: modules, each a list of objectives by their short name.
 * Every objective can be opened, in any order; the open one shows its Lesson
 * and Exercise. The plan's order is only a suggestion, marked "next".
 * Objectives being written show a spinner; several can be written at once.
 */
export function Sidebar(p: Props) {
  const { ws } = p;
  const { progress, curriculum } = ws;

  const tasks = ws.tasks ?? [];
  const objectives = progress.objectives ?? {};
  const lessons = ws.lessons ?? [];
  const openObjective = describeItem(ws, p.openItem).objective?.id;
  const inPlan = new Set((curriculum?.modules ?? []).flatMap((m) => (m.objectives ?? []).map((o) => o.id)));
  const earlier = tasks.filter((t) => !inPlan.has(t.objective));

  // The module in focus holds the open objective, else the first unfinished
  // one. Finished modules and those further ahead step back.
  const modules = curriculum?.modules ?? [];
  const isPassed = (id: string) => objectives[id]?.status === "passed";
  let focus = modules.findIndex((m) => (m.objectives ?? []).some((o) => o.id === openObjective));
  if (focus === -1) focus = modules.findIndex((m) => !(m.objectives ?? []).every((o) => isPassed(o.id)));
  // The suggested next objective: the first one not passed, in plan order.
  const suggested = modules.flatMap((m) => m.objectives ?? []).find((o) => !isPassed(o.id))?.id;

  return (
    <aside className="flex min-h-0 flex-col border-r bg-background">
      <div className="flex flex-col gap-2.5 border-b p-3.5">
        <button className="self-start text-sm text-muted-foreground hover:text-foreground" onClick={p.onBack}>
          ← Workspaces
        </button>
        <div className="flex items-center gap-2.5">
          <TopicIcon name={ws.icon} size={22} />
          <h2 className="text-[18px] leading-[1.25] font-bold wrap-anywhere">{ws.title}</h2>
        </div>
        {progress.total > 0 && (
          <>
            <ProgressBar passed={progress.passed} total={progress.total} />
            <div className="text-sm text-muted-foreground">
              {progress.passed} / {progress.total} objectives
              {progress.passedEver > progress.passed && ` · ${progress.passedEver} all-time`}
            </div>
          </>
        )}
      </div>

      <nav className="flex flex-1 flex-col gap-4 overflow-auto px-2 py-3" aria-label="Curriculum">
        {!curriculum && <p className="px-2 text-sm text-muted-foreground">{ws.proposal ? "Plan waiting for review." : "No plan yet."}</p>}
        {modules.map((m, i) => {
          const objs = m.objectives ?? [];
          const done = objs.filter((o) => isPassed(o.id)).length;
          const place = i === focus ? "focus" : done === objs.length ? "done" : "ahead";
          return (
            <section key={m.id} className={cn(place === "ahead" && "opacity-55 transition-opacity duration-160 hover:opacity-100")}>
              <h3 className={cn(moduleHeading, place === "done" && "text-muted-foreground")} title={m.title}>
                <span className="min-w-0 truncate">{m.title}</span>
                <span className="font-mono text-xs text-muted-foreground tabular-nums">
                  {done}/{objs.length}
                </span>
              </h3>
              <ul>
                {objs.map((o) => {
                  const oTasks = tasks.filter((t) => t.objective === o.id);
                  const lesson = lessons.find((l) => l.objective === o.id);
                  const passed = isPassed(o.id);
                  const open = o.id === openObjective;
                  const written = !!lesson || oTasks.length > 0;
                  const writing = p.writing.has(o.id);
                  const next = o.id === suggested && !open && !writing;
                  return (
                    <li key={o.id}>
                      <button
                        className={objRow({ done: passed, planned: !written, expanded: open, next })}
                        aria-expanded={open}
                        onClick={() => p.onOpen(entryOf(ws, o.id, p.openItem))}
                        title={writing ? `${o.title} (being written)` : o.title}
                      >
                        <Mark>{passed ? "✓" : writing && !open ? <Spinner className="inline-block size-[11px]" /> : ""}</Mark>
                        <span className="min-w-0 truncate">{o.name || o.title}</span>
                        {next && <span className="text-xs text-muted-foreground">next</span>}
                      </button>
                      {open && (
                        <Parts
                          objective={o.id}
                          lesson={lesson}
                          lessonItem={lessonOf(ws, o.id)}
                          tasks={oTasks}
                          writing={writing}
                          openItem={p.openItem}
                          onOpen={p.onOpen}
                        />
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>
          );
        })}
        {earlier.length > 0 && (
          <section>
            <h3 className={moduleHeading} title="Exercises for objectives no longer in the plan">
              Earlier exercises
            </h3>
            <ul className={subtasks}>
              {earlier.map((t) => (
                <li key={t.id}>
                  <Row
                    label={t.title}
                    done={t.passed}
                    current={p.openItem?.kind === "task" && p.openItem.id === t.id}
                    onClick={() => p.onOpen({ kind: "task", id: t.id })}
                  />
                </li>
              ))}
            </ul>
          </section>
        )}
      </nav>

      <div className="mt-auto flex justify-between border-t p-2">
        {/* The mission and the curriculum, with the planning tutor. It says it can be changed. */}
        <button
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-soft-foreground transition-colors hover:bg-muted hover:text-foreground aria-[current=page]:bg-muted aria-[current=page]:text-foreground"
          aria-current={p.planOpen ? "page" : undefined}
          onClick={p.onShowPlan}
        >
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            {/* The planning tutor wants attention: a dot after the name. */}
            <span
              className={cn(
                "text-sm font-semibold text-foreground",
                p.plan.attention &&
                  !p.planOpen &&
                  "after:ml-1.5 after:inline-block after:size-1.5 after:rounded-full after:bg-foreground after:align-middle after:content-['']",
              )}
            >
              Your plan
              {p.plan.questions > 0 && !p.planOpen && ` · ${p.plan.questions} ${p.plan.questions === 1 ? "question" : "questions"}`}
            </span>
            <span className="text-xs text-muted-foreground">Mission and curriculum. Change them anytime.</span>
          </span>
          <PencilIcon size={14} className="flex-none text-muted-foreground" />
        </button>
      </div>
    </aside>
  );
}

/** The open objective's Lesson and Exercise(s), written or not. */
function Parts({
  objective,
  lesson,
  lessonItem,
  tasks,
  writing,
  openItem,
  onOpen,
}: {
  objective: string;
  lesson?: LessonStatus;
  /** The lesson, or the page that has it written. */
  lessonItem: OpenItem;
  tasks: TaskStatus[];
  writing: boolean;
  openItem: OpenItem | null;
  onOpen: (item: OpenItem) => void;
}) {
  const exercises: { item: OpenItem; done: boolean; title?: string }[] = tasks.length
    ? tasks.map((t) => ({ item: { kind: "task", id: t.id }, done: t.passed, title: t.title }))
    : [{ item: { kind: "todo", objective, part: "exercise" }, done: false }];
  return (
    <ul className={subtasks}>
      <li>
        <Row
          label="Lesson"
          done={!!lesson && lessonDone(lesson)}
          missing={!lesson}
          writing={writing && !lesson}
          current={sameItem(openItem, lessonItem)}
          title={lesson?.title}
          onClick={() => onOpen(lessonItem)}
        />
      </li>
      {exercises.map((x, i) => (
        <li key={itemKey(x.item)}>
          <Row
            label={i === 0 ? "Exercise" : `Exercise ${i + 1}`}
            done={x.done}
            missing={!x.title}
            writing={writing && !x.title}
            current={sameItem(openItem, x.item)}
            title={x.title}
            onClick={() => onOpen(x.item)}
          />
        </li>
      ))}
    </ul>
  );
}

function Row(p: { label: string; done: boolean; missing?: boolean; writing?: boolean; current: boolean; title?: string; onClick: () => void }) {
  return (
    <button
      className={objRow({ done: p.done, planned: !!p.missing, current: p.current, sub: true })}
      aria-current={p.current ? "page" : undefined}
      onClick={p.onClick}
      title={p.title ?? (p.writing ? `${p.label}: being written` : p.missing ? `${p.label}: not written yet` : p.label)}
    >
      <Mark>{p.done ? "✓" : p.writing ? <Spinner className="inline-block size-[11px]" /> : ""}</Mark>
      <span className="min-w-0 truncate">{p.label}</span>
    </button>
  );
}

function Mark({ children }: { children: ReactNode }) {
  return <span className="text-[11px] text-foreground">{children}</span>;
}

const moduleHeading = "flex justify-between gap-2 px-2 pb-1.5 text-sm font-semibold";
const subtasks = "mt-0.5 mb-1";

/**
 * An objective or part row. Hover brightens any row; otherwise its colour
 * comes from the first that applies: not written yet (faint), the open part,
 * finished (grey), the open objective.
 */
function objRow(s: { done: boolean; planned: boolean; expanded?: boolean; current?: boolean; next?: boolean; sub?: boolean }) {
  return cn(
    "grid w-full gap-1.5 rounded-md px-2 py-[5px] text-sm leading-[1.35] transition-colors hover:bg-muted hover:text-foreground",
    s.next ? "grid-cols-[14px_1fr_auto]" : "grid-cols-[14px_1fr]",
    s.sub && "pl-7",
    s.current && "bg-accent font-medium",
    s.expanded && "font-semibold",
    s.planned ? "text-faint" : s.current ? "text-foreground" : s.done ? "text-soft-foreground" : s.expanded ? "text-foreground" : "text-muted-foreground",
  );
}
