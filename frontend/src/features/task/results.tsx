import { ChevronDownIcon } from "lucide-react";
import { cn } from "cn";
import type { TaskDetail, TaskStatus, TestResult } from "@/lib/api";
import { plural } from "@/lib/format";
import type { Run } from "@/features/task/use-task-work";
import { Button } from "@/components/ui/button";
import { ErrorText } from "@/components/error-text";
import { IconButton } from "@/components/icon-button";
import { useArmed } from "@/hooks/use-armed";

export type ResultsProps = {
  detail: TaskDetail;
  run: Run | null;
  running: "run" | "submit" | null;
  error: string;
  solved: boolean;
  nextTask: TaskStatus | null;
  moreComing: boolean;
  open: boolean;
  /** Fixed height in px when the learner has sized the results; otherwise it fits its content. */
  height?: number;
  onToggle: () => void;
  onReset: () => void;
  /** Run (visible tests) or submit (all tests). */
  onCheck: (submit: boolean) => void;
  onOpenTask: (id: string) => void;
};

/** Under the editor: the check buttons, the last result with each test, and the output. */
export function Results(p: ResultsProps) {
  const { detail, run, running, error, solved } = p;
  const [resetArmed, armReset] = useArmed();
  const r = run?.result;
  const tests = r?.tests ?? [];
  const passedCount = tests.filter((t) => t.passed).length;
  const notRun = tests.filter((t) => t.notRun).length;
  const firstFail = tests.findIndex((t) => !t.passed && !t.notRun);
  const state = r ? (STATES[r.status] ?? STATES.error) : null;

  return (
    // Under the y splitter the splitter is the top edge, so no border of its own.
    <div className={cn("flex min-h-0 flex-none flex-col bg-card", !p.height && "border-t")} style={p.height ? { height: p.height } : undefined}>
      <div className="flex min-h-12 flex-none items-center gap-3 py-2 pr-2 pl-3.5 text-sm">
        {running ? (
          <span className="flex-none whitespace-nowrap text-muted-foreground">{running === "submit" ? "Running all tests…" : "Running visible tests…"}</span>
        ) : state ? (
          <>
            <b className={cn("flex-none font-semibold whitespace-nowrap", state.tone)}>{state.label}</b>
            <span className="min-w-0 truncate font-mono text-xs text-muted-foreground tabular-nums">
              {tests.length > 0 && `${passedCount}/${tests.length} tests · `}
              {(r!.durationMs / 1000).toFixed(1)}s{notRun > 0 && ` · ${notRun} not run`}
            </span>
          </>
        ) : (
          <span className="min-w-0 truncate text-muted-foreground" title="Run checks the visible tests. Submit runs all of them, including hidden ones.">
            {detail.hiddenCount > 0 ? `Submit adds ${plural(detail.hiddenCount, "hidden test")}` : ""}
          </span>
        )}
        <span className="flex-1" />
        <Button
          variant="ghost"
          className={cn("-mx-1.5", resetArmed && "text-fail")}
          onClick={() => (resetArmed ? p.onReset() : armReset())}
          disabled={!!running}
          title="Replace your code with the starter code"
        >
          {resetArmed ? "Discard your code?" : "Reset"}
        </Button>
        <Button variant="outline" onClick={() => p.onCheck(false)} disabled={!!running} title="Run the visible tests">
          {running === "run" ? "Running…" : "Run"} <kbd>{KEY_RUN}</kbd>
        </Button>
        {/* Once passed, Next is the main action and Submit steps back. */}
        <Button variant={solved ? "outline" : "default"} onClick={() => p.onCheck(true)} disabled={!!running} title="Run all tests, including hidden ones">
          {running === "submit" ? "Submitting…" : "Submit"} <kbd>{KEY_SUBMIT}</kbd>
        </Button>
        {solved &&
          (p.nextTask ? (
            <Button onClick={() => p.onOpenTask(p.nextTask!.id)} title={p.nextTask.title}>
              Next exercise →
            </Button>
          ) : p.moreComing ? (
            <span className="flex-none whitespace-nowrap text-muted-foreground">Preparing next exercise…</span>
          ) : (
            <span className="flex-none whitespace-nowrap text-muted-foreground">Plan complete</span>
          ))}
        <IconButton label={p.open ? "Hide results" : "Show results"} onClick={p.onToggle} aria-expanded={p.open}>
          <ChevronDownIcon size={16} className={cn("transition-transform", !p.open && "rotate-180")} />
        </IconButton>
      </div>

      {p.open && <ErrorText error={error} className="px-3.5 pb-2.5" />}

      {p.open && r && (
        <div className="min-h-0 flex-1 overflow-auto border-t px-3.5 pb-3">
          {tests.length > 0 && (
            <ul className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-x-[18px] pt-1">
              {tests.map((t, i) => {
                const row = (
                  <>
                    <TestMark test={t} />
                    <span className="min-w-0 truncate">{t.name}</span>
                    {t.notRun && <span className={cn(ROW_NOTE, "text-muted-foreground")}>didn't run</span>}
                    {t.hidden && <span className={cn(ROW_NOTE, "ml-auto text-muted-foreground")}>hidden</span>}
                  </>
                );
                // A failure's message folds out under its row; the first one starts open.
                const failed = !t.passed && !t.notRun && !!t.message;
                return (
                  <li key={`${t.hidden ? "h" : "v"}:${t.name}`} className={cn(failed && "col-span-full")}>
                    {failed ? (
                      <details open={i === firstFail}>
                        <summary className={cn(TEST_ROW, "cursor-pointer list-none [&::-webkit-details-marker]:hidden")}>{row}</summary>
                        <pre className="border-b pt-1.5 pb-2.5 pl-5 text-xs leading-[1.55] wrap-anywhere whitespace-pre-wrap text-foreground">{t.message}</pre>
                      </details>
                    ) : (
                      <div className={TEST_ROW} title={t.notRun ? t.message : undefined}>
                        {row}
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          {r.output && (
            <details open={r.status === "error"}>
              <summary className="cursor-pointer pt-2.5 pb-1 text-xs text-muted-foreground hover:text-foreground">Output</summary>
              <pre className="mt-1 max-h-[280px] overflow-auto rounded-[8px] border bg-background px-3 py-2.5 text-xs leading-[1.55] whitespace-pre-wrap text-soft-foreground">
                {r.output}
              </pre>
            </details>
          )}
        </div>
      )}
    </div>
  );
}

const TEST_ROW = "flex items-baseline gap-2 border-b py-[5px] font-mono text-xs leading-[normal] text-soft-foreground";
/** Words in a test row are set in the sans face, unlike the test's name. */
const ROW_NOTE = "font-sans text-xs leading-[normal]";

function TestMark({ test }: { test: TestResult }) {
  if (test.notRun)
    return (
      <span className={cn("w-3 flex-none text-muted-foreground", ROW_NOTE)} aria-label="Didn't run">
        –
      </span>
    );
  return (
    <span className={cn("w-3 flex-none", test.passed ? "text-pass" : "text-fail")} aria-label={test.passed ? "Passed" : "Failed"}>
      {test.passed ? "✓" : "✗"}
    </span>
  );
}

const STATES: Record<string, { label: string; tone: string }> = {
  passed: { label: "Passed", tone: "text-pass" },
  failed: { label: "Failed", tone: "text-fail" },
  error: { label: "Error", tone: "text-fail" },
};

const MAC = /Mac/.test(navigator.platform);
const KEY_RUN = MAC ? "⌘↵" : "Ctrl↵";
const KEY_SUBMIT = MAC ? "⇧⌘↵" : "⇧Ctrl↵";
