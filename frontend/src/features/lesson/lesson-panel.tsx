import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";
import { LessonService, errorText, type LessonDetail, type QuizResult, type Source } from "@/lib/api";
import { plural } from "@/lib/format";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import { Markdown } from "@/components/markdown";
import { References } from "@/components/references";
import { TopicIcon } from "@/components/topic-icon";
import { ErrorText } from "@/components/error-text";
import { Placeholder } from "@/components/placeholder";
import { Page, PageHeader, ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

type Props = {
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
 * the topic can skip ahead. Where the lesson comes from is one tab away, so
 * it doesn't interrupt the reading.
 */
export function LessonPanel({ lessonId, moduleTitle, icon, lang, onOpenExercise, exerciseWritten, references }: Props) {
  const { id: wsId } = useLiveWorkspace();
  const [lesson, setLesson] = useState<LessonDetail | null>(null);
  const [results, setResults] = useState<Record<number, QuizResult>>({});
  const [error, setError] = useState("");

  useEffect(() => {
    LessonService.Lesson(wsId, lessonId)
      .then((l) => {
        setLesson(l);
        setResults(Object.fromEntries(Object.entries(l.results ?? {}).map(([k, v]) => [Number(k), v!])));
      })
      .catch((e) => setError(errorText(e)));
  }, [wsId, lessonId]);

  async function answer(question: number, choice: number) {
    if (results[question]) return;
    try {
      const r = await LessonService.AnswerQuiz(wsId, lessonId, question, choice);
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
    <Button variant={done && exerciseWritten ? "default" : "outline"} onClick={onOpenExercise}>
      {done ? "Start the exercise →" : "Skip to the exercise →"}
    </Button>
  );

  return (
    <Page>
      <PageHeader module={moduleTitle} label="Lesson:" title={lesson.title}>
        {toExercise}
      </PageHeader>

      <Tabs defaultValue="lesson" className="min-h-0 flex-1">
        <TabsList aria-label="Lesson" className="px-12">
          <TabsTrigger value="lesson">Lesson</TabsTrigger>
          <TabsTrigger value="sources">Sources</TabsTrigger>
        </TabsList>
        <TabsContent value="lesson" className="flex min-h-0 flex-col">
          {/* Lessons are for reading: larger text, more air, and roomier markdown than elsewhere. */}
          <ReadingPage
            className={cn(
              "text-[16px] [&>*+*]:mt-7",
              "[&_.prose]:leading-[1.7] [&_.prose_code]:text-[13px] [&_.prose_pre]:text-[13px]",
              "[&_.prose_h2]:mt-7 [&_.prose_h2]:text-[17px] [&_.prose_h2]:font-bold [&_.prose_h3]:mt-7 [&_.prose_h3]:text-[17px] [&_.prose_h3]:font-bold",
            )}
          >
            <header className="flex flex-col gap-2.5 pt-2">
              <span className="inline-flex items-center gap-2 text-sm text-muted-foreground">
                <TopicIcon name={icon} size={16} />
                {moduleTitle ? `${moduleTitle} · Lesson` : "Lesson"}
              </span>
              <h1 className="text-[36px] leading-[1.12] font-bold tracking-[-0.01em] wrap-anywhere text-foreground">{lesson.title}</h1>
            </header>
            <article className="prose">
              <p className="text-[16px] text-foreground">{lesson.summary}</p>
              <Markdown text={lesson.body} className="prose" lang={lang} />
            </article>

            {quiz.length > 0 && (
              <section aria-labelledby="quiz-title">
                <h2 id="quiz-title" className="text-[20px] leading-[1.25] font-bold text-foreground">
                  Check yourself
                </h2>
                <p className="text-sm text-muted-foreground">Answer from memory. Only your first answer counts, and it's only for you and your tutor.</p>
                <ol className="mt-3.5 flex flex-col gap-[22px]">
                  {quiz.map((q, i) => {
                    const r = results[i];
                    return (
                      <li key={i} className="flex flex-col gap-2.5">
                        <Markdown text={q.question} className="prose font-medium text-foreground" lang={lang} />
                        <div className="flex flex-col gap-1.5" role="group" aria-label={`Question ${i + 1}`}>
                          {(q.options ?? []).map((o, j) => {
                            const state = !r ? "open" : j === r.answer ? "right" : j === r.choice ? "wrong" : "dim";
                            return (
                              <button
                                key={j}
                                className={cn(
                                  "grid grid-cols-[16px_1fr] items-baseline gap-2 rounded-[8px] border border-input bg-card px-3 py-[9px] text-left text-soft-foreground transition-colors",
                                  "enabled:hover:border-foreground enabled:hover:text-foreground disabled:cursor-default",
                                  state === "right" && "border-foreground text-foreground",
                                  state === "dim" && "text-faint",
                                )}
                                disabled={!!r}
                                onClick={() => answer(i, j)}
                                aria-pressed={r?.choice === j}
                              >
                                <span className={cn(state === "right" && "text-pass", state === "wrong" && "text-fail")} aria-hidden="true">
                                  {state === "right" ? "✓" : state === "wrong" ? "✗" : ""}
                                </span>
                                <span className="[&_code]:font-mono [&_code]:text-[0.92em] [&_code]:leading-[normal]">
                                  <InlineCode text={o} />
                                </span>
                              </button>
                            );
                          })}
                        </div>
                        {r && (
                          <div className="flex flex-col gap-1 border-l-2 border-input pl-3 text-sm" role="status">
                            <b className={r.correct ? "text-pass" : "text-fail"}>{r.correct ? "Right" : "Not quite"}</b>
                            <Markdown text={r.explanation} className="prose" lang={lang} />
                          </div>
                        )}
                      </li>
                    );
                  })}
                </ol>
                {done && (
                  <div className="mt-[22px] flex items-center justify-between gap-3 border-t pt-3.5">
                    <span className="font-medium tabular-nums">
                      {correct} / {quiz.length} right
                    </span>
                    {toExercise}
                  </div>
                )}
                {!done && answered > 0 && <p className="text-sm text-muted-foreground">{plural(quiz.length - answered, "question")} to go.</p>}
              </section>
            )}
            <p className="pb-6 text-sm text-muted-foreground">Anything unclear? Ask the tutor about this lesson.</p>
            <ErrorText error={error} />
          </ReadingPage>
        </TabsContent>
        <TabsContent value="sources" className="flex min-h-0 flex-col">
          <ReadingPage>
            <LessonSources lesson={lesson} references={references} />
          </ReadingPage>
        </TabsContent>
      </Tabs>
    </Page>
  );
}

/**
 * Where the lesson comes from: the one page to read next, the pages the
 * lesson is based on, and its module's references. Lessons written before
 * they kept their sources show only the other two.
 */
function LessonSources({ lesson, references }: { lesson: LessonDetail; references?: Source[] | null }) {
  const next = lesson.primarySource?.url ? [lesson.primarySource] : [];
  if (!next.length && !lesson.sources?.length && !references?.length) {
    return <p className="text-sm text-muted-foreground">This lesson cites its sources inline, next to the claims they support.</p>;
  }
  return (
    <div className="flex flex-col gap-7">
      <p className="text-sm text-muted-foreground">The lesson also links its sources next to the claims they support.</p>
      <References sources={next} label="Read next" />
      <References sources={lesson.sources} label="This lesson is based on" />
      <References sources={references} label="This module is based on" />
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
