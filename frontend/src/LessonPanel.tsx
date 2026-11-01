import { useEffect, useState } from "react";
import { App, errorText, plural, type LessonDetail, type QuizResult, type Source } from "./api";
import { Link, Markdown } from "./Markdown";
import { References } from "./References";
import { TopicIcon } from "./topicIcons";
import { ErrorText, Placeholder } from "./ui";

type Props = {
  wsId: string;
  lessonId: string;
  moduleTitle: string;
  /** The workspace's icon, shown small above the title; nothing until one is picked. */
  icon: string;
  /** The objective's language, for colouring code. */
  lang: string;
  /** Opens the objective's exercise, or the page that asks for it if it isn't written yet. */
  onOpenExercise: () => void;
  exerciseWritten: boolean;
  /** The official pages the lesson's module is based on. */
  references?: Source[] | null;
};

/**
 * A lesson: the theory for one objective, then a quiz with instant feedback,
 * then on to the exercise. The exercise is always reachable, so a learner who knows
 * the topic can skip ahead.
 */
export function LessonPanel({ wsId, lessonId, moduleTitle, icon, lang, onOpenExercise, exerciseWritten, references }: Props) {
  const [lesson, setLesson] = useState<LessonDetail | null>(null);
  const [results, setResults] = useState<Record<number, QuizResult>>({});
  const [error, setError] = useState("");

  useEffect(() => {
    App.Lesson(wsId, lessonId)
      .then((l) => {
        setLesson(l);
        setResults(Object.fromEntries(Object.entries(l.results ?? {}).map(([k, v]) => [Number(k), v!])));
      })
      .catch((e) => setError(errorText(e)));
  }, [wsId, lessonId]);

  async function answer(question: number, choice: number) {
    if (results[question]) return;
    try {
      const r = await App.AnswerQuiz(wsId, lessonId, question, choice);
      setResults((cur) => ({ ...cur, [question]: r }));
    } catch (e) {
      setError(errorText(e));
    }
  }

  if (!lesson) return <Placeholder error={error} loading="Loading lesson…" />;

  const quiz = lesson.quiz ?? [];
  const answered = Object.keys(results).length;
  const correct = Object.values(results).filter((r) => r.correct).length;
  const done = answered >= quiz.length;
  const toExercise = (
    <button className={`btn ${done && exerciseWritten ? "ink" : ""}`} onClick={onOpenExercise}>
      {done ? "Start the exercise →" : "Skip to the exercise →"}
    </button>
  );

  return (
    <div className="task">
      <header className="taskbar">
        <div className="crumbs truncate" title={lesson.title}>
          {moduleTitle && <span className="muted">{moduleTitle} / </span>}
          <span className="muted">Lesson: </span>
          <b>{lesson.title}</b>
        </div>
        {toExercise}
      </header>

      <div className="reading lesson">
        {/* A plain headline: the module above it, with the workspace's logo if it has one. */}
        <header className="lesson-head">
          <span className="lesson-kicker">
            <TopicIcon name={icon} size={16} />
            {moduleTitle ? `${moduleTitle} · Lesson` : "Lesson"}
          </span>
          <h1>{lesson.title}</h1>
        </header>
        <article className="prose">
          <p className="lesson-summary">{lesson.summary}</p>
          <Markdown text={lesson.body} className="prose" lang={lang} />
        </article>

        {/* Where this comes from: the lesson cites its sources inline; these are the module's, and the one to read next. */}
        {(lesson.primarySource?.url || !!references?.length) && (
          <aside className="lesson-source">
            {lesson.primarySource?.url && (
              <div>
                <span className="field-label">Read next</span>
                <p className="prose">
                  <Link href={lesson.primarySource.url}>{lesson.primarySource.title}</Link>
                </p>
              </div>
            )}
            <References sources={references} label="This module is based on" />
          </aside>
        )}

        {quiz.length > 0 && (
          <section className="quiz" aria-labelledby="quiz-title">
            <h2 id="quiz-title" className="quiz-title">
              Check yourself
            </h2>
            <p className="muted small">Answer from memory. Only your first answer counts, and it's only for you and your tutor.</p>
            <ol className="quiz-list">
              {quiz.map((q, i) => {
                const r = results[i];
                return (
                  <li key={i} className="quiz-q">
                    <Markdown text={q.question} className="prose quiz-question" lang={lang} />
                    <div className="quiz-options" role="group" aria-label={`Question ${i + 1}`}>
                      {(q.options ?? []).map((o, j) => {
                        const state = !r ? "" : j === r.answer ? "is-right" : j === r.choice ? "is-wrong" : "is-dim";
                        return (
                          <button key={j} className={`quiz-option ${state}`} disabled={!!r} onClick={() => answer(i, j)} aria-pressed={r?.choice === j}>
                            <span className="quiz-mark" aria-hidden="true">
                              {state === "is-right" ? "✓" : state === "is-wrong" ? "✗" : ""}
                            </span>
                            <span className="quiz-option-text">
                              <InlineCode text={o} />
                            </span>
                          </button>
                        );
                      })}
                    </div>
                    {r && (
                      <div className="quiz-feedback" role="status">
                        <b className={r.correct ? "pass" : "fail"}>{r.correct ? "Right" : "Not quite"}</b>
                        <Markdown text={r.explanation} className="prose" lang={lang} />
                      </div>
                    )}
                  </li>
                );
              })}
            </ol>
            {done && (
              <div className="quiz-done">
                <span className="num-strong">
                  {correct} / {quiz.length} right
                </span>
                {toExercise}
              </div>
            )}
            {!done && answered > 0 && <p className="muted small">{plural(quiz.length - answered, "question")} to go.</p>}
          </section>
        )}
        <p className="muted small lesson-ask">Anything unclear? Ask the tutor about this lesson.</p>
        <ErrorText error={error} />
      </div>
    </div>
  );
}

/** Text with `backtick` spans as code: quiz options sit inside buttons, so no block markdown. */
function InlineCode({ text }: { text: string }) {
  return (
    <>
      {text
        .split(/(`[^`]+`)/)
        .map((part, i) => (part.startsWith("`") && part.endsWith("`") && part.length > 2 ? <code key={i}>{part.slice(1, -1)}</code> : part))}
    </>
  );
}
