import { exerciseTarget, lessonDone, lessonKey, lessonOfKey, lessonTarget, todoKey, todoOfKey, type LessonStatus, type TaskInfo, type WorkspaceState } from "./api";
import { Pencil, Spinner } from "./icons";
import { TopicIcon } from "./topicIcons";
import { ProgressBar } from "./ui";

type Props = {
  ws: WorkspaceState;
  openTaskId: string | null;
  planOpen: boolean;
  /** The planning coach wants attention: new messages, work, or questions waiting. */
  plan: { attention: boolean; questions: number };
  /** Objectives whose lesson and exercise are being written in the background. */
  writing: Set<string>;
  onBack: () => void;
  onOpenTask: (id: string) => void;
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
  const openKey = p.openTaskId;
  const openLessonId = lessonOfKey(openKey);
  const openObjective =
    todoOfKey(openKey)?.objective ?? lessons.find((l) => l.id === openLessonId)?.objective ?? tasks.find((t) => t.id === openKey)?.objective;
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
    <aside className="rail">
      <div className="rail-head">
        <button className="back" onClick={p.onBack}>
          ← Workspaces
        </button>
        <div className="rail-title">
          <TopicIcon name={ws.icon} size={22} />
          <h2 className="rail-name">{ws.title}</h2>
        </div>
        {progress.total > 0 && (
          <>
            <ProgressBar passed={progress.passed} total={progress.total} />
            <div className="muted small">
              {progress.passed} / {progress.total} objectives
              {progress.passedEver > progress.passed && ` · ${progress.passedEver} all-time`}
            </div>
          </>
        )}
      </div>

      <nav className="tree" aria-label="Curriculum">
        {!curriculum && <p className="muted small">{ws.proposal ? "Plan waiting for review." : "No plan yet."}</p>}
        {modules.map((m, i) => {
          const objs = m.objectives ?? [];
          const done = objs.filter((o) => isPassed(o.id)).length;
          const place = i === focus ? "is-focus" : done === objs.length ? "is-done" : "is-ahead";
          return (
            <section key={m.id} className={`tree-module ${place}`}>
              <h3 className="module" title={m.title}>
                <span className="truncate">{m.title}</span>
                <span className="num">
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
                  return (
                    <li key={o.id}>
                      <button
                        className={`obj ${passed ? "is-done" : ""} ${written ? "" : "is-planned"}`}
                        aria-expanded={open}
                        onClick={() => p.onOpenTask(entry(o.id, passed, lesson, oTasks, openKey))}
                        title={writing ? `${o.title} (being written)` : o.title}
                      >
                        <span className="mark">{passed ? "✓" : writing && !open ? <Spinner size={11} /> : ""}</span>
                        <span className="truncate">{o.name || o.title}</span>
                        {o.id === suggested && !open && !writing && <span className="obj-next">next</span>}
                      </button>
                      {open && <Parts objective={o.id} lesson={lesson} tasks={oTasks} writing={writing} openKey={openKey} onOpen={p.onOpenTask} />}
                    </li>
                  );
                })}
              </ul>
            </section>
          );
        })}
        {earlier.length > 0 && (
          <section>
            <h3 className="module" title="Exercises for objectives no longer in the plan">
              Earlier exercises
            </h3>
            <ul className="subtasks">
              {earlier.map((t) => (
                <li key={t.id}>
                  <Row label={t.title} done={t.passed} current={t.id === openKey} onClick={() => p.onOpenTask(t.id)} />
                </li>
              ))}
            </ul>
          </section>
        )}
      </nav>

      <div className="rail-foot">
        {/* The mission and the curriculum, with the planning tutor. It says it can be changed. */}
        <button
          className={`plan-link ${p.plan.attention && !p.planOpen ? "is-loud" : ""}`}
          aria-current={p.planOpen ? "page" : undefined}
          onClick={p.onShowPlan}
        >
          <span className="plan-link-text">
            <span className="plan-link-name">
              Your plan
              {p.plan.questions > 0 && !p.planOpen && ` · ${p.plan.questions} ${p.plan.questions === 1 ? "question" : "questions"}`}
            </span>
            <span className="plan-link-hint">Mission and curriculum. Change them anytime.</span>
          </span>
          <Pencil size={14} />
        </button>
      </div>
    </aside>
  );
}

/**
 * What clicking an objective opens, following the suggested flow: its lesson
 * until the quiz is done, then its exercise; else whatever exists, else the
 * lesson page that asks the coach to write it.
 */
function entry(objective: string, passed: boolean, lesson: LessonStatus | undefined, tasks: TaskInfo[], openKey: string | null): string {
  if (lesson && !passed && !lessonDone(lesson)) return lessonKey(lesson.id);
  const open = tasks.find((t) => t.id === openKey);
  if (open) return open.id;
  return tasks.length ? exerciseTarget(objective, tasks) : lessonTarget(objective, lesson);
}

/** The open objective's Lesson and Exercise(s), written or not. */
function Parts({
  objective,
  lesson,
  tasks,
  writing,
  openKey,
  onOpen,
}: {
  objective: string;
  lesson?: LessonStatus;
  tasks: TaskInfo[];
  writing: boolean;
  openKey: string | null;
  onOpen: (key: string) => void;
}) {
  const lessonItem = lessonTarget(objective, lesson);
  const exercises: { key: string; done: boolean; title?: string }[] = tasks.length
    ? tasks.map((t) => ({ key: t.id, done: t.passed, title: t.title }))
    : [{ key: todoKey(objective, "exercise"), done: false }];
  return (
    <ul className="subtasks">
      <li>
        <Row
          label="Lesson"
          done={!!lesson && lessonDone(lesson)}
          missing={!lesson}
          writing={writing && !lesson}
          current={openKey === lessonItem}
          title={lesson?.title}
          onClick={() => onOpen(lessonItem)}
        />
      </li>
      {exercises.map((x, i) => (
        <li key={x.key}>
          <Row
            label={i === 0 ? "Exercise" : `Exercise ${i + 1}`}
            done={x.done}
            missing={!x.title}
            writing={writing && !x.title}
            current={openKey === x.key}
            title={x.title}
            onClick={() => onOpen(x.key)}
          />
        </li>
      ))}
    </ul>
  );
}

function Row(p: { label: string; done: boolean; missing?: boolean; writing?: boolean; current: boolean; title?: string; onClick: () => void }) {
  return (
    <button
      className={`obj ${p.done ? "is-done" : ""} ${p.missing ? "is-planned" : ""}`}
      aria-current={p.current ? "page" : undefined}
      onClick={p.onClick}
      title={p.title ?? (p.writing ? `${p.label}: being written` : p.missing ? `${p.label}: not written yet` : p.label)}
    >
      <span className="mark">{p.done ? "✓" : p.writing ? <Spinner size={11} /> : ""}</span>
      <span className="truncate">{p.label}</span>
    </button>
  );
}
