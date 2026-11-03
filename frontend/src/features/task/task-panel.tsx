import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import Editor from "@monaco-editor/react";
import { App, errorText, type Result, type TaskDetail, type TaskInfo, type TestResult } from "@/lib/api";
import { plural } from "@/lib/format";
import { editorOptions, monaco } from "@/lib/monaco";
import { LanguageClient, modelPath, type LanguageStatus } from "@/features/task/lsp";
import { Markdown } from "@/components/markdown";
import { ChevronDownIcon } from "lucide-react";
import { cn } from "cn";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ErrorText } from "@/components/error-text";
import { IconButton } from "@/components/icon-button";
import { Page, PageHeader } from "@/components/page";
import { Placeholder } from "@/components/placeholder";
import { Splitter, clamp } from "@/components/splitter";
import { useArmed } from "@/hooks/use-armed";
import { useStored } from "@/hooks/use-stored";

type Props = {
  wsId: string;
  taskId: string;
  /** From the workspace state; kept current by "changed" events. */
  passed: boolean;
  /** The module the task's objective belongs to, for the task bar. */
  moduleTitle: string;
  /** The first unsolved task after this one, if the tutor has written it yet. */
  nextTask: TaskInfo | null;
  /** The plan has objectives left, so a next task is coming if there isn't one yet. */
  moreComing: boolean;
  onOpenTask: (id: string) => void;
};

type Run = { result: Result; submit: boolean };

// The latest check per task, so switching tasks doesn't lose it.
const lastRuns = new Map<string, Run>();

const MAC = /Mac/.test(navigator.platform);
const KEY_RUN = MAC ? "⌘↵" : "Ctrl↵";
const KEY_SUBMIT = MAC ? "⇧⌘↵" : "⇧Ctrl↵";

/** The learner's current code for each editable file. */
const workOf = (d: TaskDetail) => Object.fromEntries((d.files ?? []).map((f) => [f, d.work?.[f] ?? ""]));

export function TaskPanel({ wsId, taskId, passed, moduleTitle, nextTask, moreComing, onOpenTask }: Props) {
  const runKey = `${wsId}/${taskId}`;
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const [files, setFiles] = useState<Record<string, string>>({});
  const [activeFile, setActiveFile] = useState("");
  const [tab, setTab] = useState<"statement" | "tests">("statement");
  const [activeTest, setActiveTest] = useState("");
  const [run, setRun] = useState<Run | null>(lastRuns.get(runKey) ?? null);
  const [running, setRunning] = useState<"run" | "submit" | null>(null);
  const [dirty, setDirty] = useState(false); // edits not yet saved
  const [error, setError] = useState("");
  const [lspStatus, setLspStatus] = useState<LanguageStatus>("off");

  // Layout the learner can adjust; remembered across tasks and restarts.
  const [split, setSplit] = useStored("split", 0.45); // statement's share of the width
  const [resultsOpen, setResultsOpen] = useStored("results.open", true);
  const [resultsHeight, setResultsHeight] = useStored("results.height", 220);
  const bodyRef = useRef<HTMLDivElement>(null);
  const workRef = useRef<HTMLElement>(null);

  const filesRef = useRef(files);
  const unsaved = useRef<Record<string, string> | null>(null);
  const saveTimer = useRef<number | undefined>(undefined);
  const runningRef = useRef(false);

  useEffect(() => {
    App.Task(wsId, taskId)
      .then((d) => {
        setDetail(d);
        replaceFiles(workOf(d));
        setActiveFile(d.files?.[0] ?? "");
        setActiveTest(Object.keys(d.tests ?? {}).sort()[0] ?? "");
      })
      .catch((e) => setError(errorText(e)));
    // Flush unsaved edits when leaving the task.
    return () => {
      window.clearTimeout(saveTimer.current);
      save();
    };
  }, [wsId, taskId]);

  // A language server for completions, hover docs and errors, while the task is open.
  const editorLanguage = detail?.editorLanguage || "plaintext";
  const hasLanguageServer = !!detail?.hasLanguageServer;
  useEffect(() => {
    if (!hasLanguageServer) return;
    const client = new LanguageClient(wsId, taskId, editorLanguage, setLspStatus);
    client.start();
    return () => {
      client.dispose();
      setLspStatus("off");
    };
  }, [wsId, taskId, editorLanguage, hasLanguageServer]);

  // filesRef mirrors files because Monaco's onChange can run with a stale closure.
  function replaceFiles(work: Record<string, string>) {
    filesRef.current = work;
    setFiles(work);
  }

  function cancelSave() {
    window.clearTimeout(saveTimer.current);
    unsaved.current = null;
  }

  function save() {
    const pending = unsaved.current;
    if (!pending) return;
    unsaved.current = null;
    App.SaveWork(wsId, taskId, pending)
      .then(() => !unsaved.current && setDirty(false))
      .catch((e) => setError(`Couldn't save: ${errorText(e)}`));
  }

  function edit(name: string, value: string) {
    replaceFiles({ ...filesRef.current, [name]: value });
    unsaved.current = filesRef.current;
    setDirty(true);
    window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(save, 800);
  }

  async function check(submit: boolean) {
    if (runningRef.current || !detail) return;
    runningRef.current = true;
    setRunning(submit ? "submit" : "run");
    setError("");
    cancelSave(); // RunChecks saves the files itself.
    try {
      const result = await App.RunChecks(wsId, taskId, filesRef.current, submit);
      const r = { result, submit };
      lastRuns.set(runKey, r);
      setRun(r);
      setResultsOpen(true);
      setDirty(false);
      if (submit && result.status === "passed") setDetail((d) => d && { ...d, passed: true });
    } catch (e) {
      setError(errorText(e));
      setResultsOpen(true);
      unsaved.current = filesRef.current;
      save();
    } finally {
      runningRef.current = false;
      setRunning(null);
    }
  }

  // Keyboard: Ctrl/Cmd+Enter runs, with Shift it submits. Monaco gets the same
  // bindings as actions below, since it swallows Ctrl+Enter itself.
  const checkRef = useRef(check);
  checkRef.current = check;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.key !== "Enter" || !(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      checkRef.current(e.shiftKey);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  async function reset() {
    cancelSave();
    try {
      await App.ResetWork(wsId, taskId);
      replaceFiles(workOf(await App.Task(wsId, taskId)));
      setDirty(false);
    } catch (e) {
      setError(errorText(e));
    }
  }

  // Splitters: the statement keeps at least 260px, the editor at least 340px,
  // and the editor keeps at least 140px above the results. Dragging and the
  // arrow keys both go through these setters, so they share the bounds.
  function sizeSplit(fraction: number) {
    const w = bodyRef.current!.getBoundingClientRect().width;
    setSplit(clamp(fraction, 260 / w, 1 - 340 / w));
  }
  function sizeResults(height: number) {
    const h = workRef.current!.getBoundingClientRect().height;
    setResultsHeight(Math.round(clamp(height, 96, h - 140)));
  }

  if (!detail) return <Placeholder error={error} loading="Loading exercise…" />;

  const editable = detail.files ?? [];
  const testFiles = Object.keys(detail.tests ?? {}).sort();
  const lang = editorLanguage;
  const solved = passed || detail.passed;
  const sizedResults = resultsOpen && !!(run || error);

  return (
    <Page>
      <PageHeader module={moduleTitle} title={detail.title} />

      <div className="flex min-h-0 flex-1 @max-[640px]:flex-col" ref={bodyRef}>
        {/* Below 640px the statement stacks over the editor at a fixed share of the height. */}
        <Tabs
          value={tab}
          onValueChange={(v) => setTab(v as typeof tab)}
          className="flex min-h-0 min-w-[260px] flex-none flex-col gap-0 @max-[640px]:h-2/5 @max-[640px]:w-auto! @max-[640px]:min-w-0 @max-[640px]:border-b"
          style={{ width: `${split * 100}%` }}
        >
          <TabsList
            variant="line"
            aria-label="Exercise"
            className="flex w-full flex-none items-stretch justify-start gap-[18px] border-b px-[22px] py-0 group-data-horizontal/tabs:h-10"
          >
            <TabsTrigger value="statement" className={TAB}>
              Exercise
            </TabsTrigger>
            <TabsTrigger value="tests" className={TAB}>
              Tests
            </TabsTrigger>
          </TabsList>
          <TabsContent value="statement" className="flex-1 overflow-auto text-base">
            <Markdown text={detail.statement} className="prose px-[26px] pt-[22px] pb-10" foldExamples lang={detail.editorLanguage} />
          </TabsContent>
          <TabsContent value="tests" className="flex min-h-0 flex-1 flex-col text-base">
            <FileTabs label="Test files" files={testFiles} active={activeTest} onSelect={setActiveTest}>
              {detail.hiddenCount > 0 && <FileNote>+ {plural(detail.hiddenCount, "hidden test")} on submit</FileNote>}
            </FileTabs>
            <div className={EDITOR_HOST}>
              {activeTest && (
                <Editor
                  path={modelPath(taskId, "tests", activeTest)}
                  language={lang}
                  value={detail.tests?.[activeTest] ?? ""}
                  theme="ink"
                  options={{ ...editorOptions, readOnly: true, domReadOnly: true, wordWrap: "on" }}
                />
              )}
            </div>
          </TabsContent>
        </Tabs>

        <Splitter
          axis="x"
          label="Resize statement and editor"
          className="@max-[640px]:hidden"
          onDrag={(x) => {
            const r = bodyRef.current!.getBoundingClientRect();
            sizeSplit((x - r.left) / r.width);
          }}
          onStep={(d) => sizeSplit(split + d * 0.03)}
        />

        <section className="flex min-h-0 min-w-0 flex-1 flex-col" ref={workRef}>
          <FileTabs label="Your files" files={editable} active={activeFile} onSelect={setActiveFile}>
            <LspNote status={lspStatus} />
            <FileNote>{dirty ? "Saving…" : "Saved"}</FileNote>
          </FileTabs>
          <div className={EDITOR_HOST}>
            {activeFile && (
              <Editor
                path={modelPath(taskId, "work", activeFile)}
                language={lang}
                value={files[activeFile] ?? ""}
                theme="ink"
                options={editorOptions}
                onChange={(v) => edit(activeFile, v ?? "")}
                onMount={(editor) => {
                  editor.addAction({
                    id: "tutortop.run",
                    label: "Run tests",
                    keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
                    run: () => void checkRef.current(false),
                  });
                  editor.addAction({
                    id: "tutortop.submit",
                    label: "Submit",
                    keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.Enter],
                    run: () => void checkRef.current(true),
                  });
                }}
              />
            )}
          </div>
          {sizedResults && (
            <Splitter
              axis="y"
              label="Resize editor and results"
              onDrag={(y) => sizeResults(workRef.current!.getBoundingClientRect().bottom - y)}
              onStep={(d) => sizeResults(resultsHeight - d * 24)}
            />
          )}
          <Results
            detail={detail}
            run={run}
            running={running}
            error={error}
            solved={solved}
            nextTask={nextTask}
            moreComing={moreComing}
            open={resultsOpen}
            height={sizedResults ? resultsHeight : undefined}
            onToggle={() => setResultsOpen(!resultsOpen)}
            onReset={reset}
            onCheck={check}
            onOpenTask={onOpenTask}
          />
        </section>
      </div>
    </Page>
  );
}

/** The Exercise | Tests underline tabs. */
const TAB =
  "-mb-px h-auto flex-none rounded-none border-0 border-b-2 border-transparent px-0 py-0 text-muted-foreground transition-colors hover:text-foreground data-active:border-foreground data-active:text-foreground";

const EDITOR_HOST = "min-h-0 flex-1 overflow-hidden bg-card";

/** A row of file tabs, with optional trailing content (save state, notes). */
function FileTabs({
  label,
  files,
  active,
  onSelect,
  children,
}: {
  label: string;
  files: string[];
  active: string;
  onSelect: (file: string) => void;
  children?: ReactNode;
}) {
  return (
    <div className="flex h-10 flex-none overflow-x-auto border-b px-2.5" role="tablist" aria-label={label}>
      {files.map((f) => (
        <button
          key={f}
          role="tab"
          aria-selected={active === f}
          className="-mb-px flex items-center border-b-2 border-transparent px-3 font-mono text-xs whitespace-nowrap text-muted-foreground hover:text-foreground aria-selected:border-foreground aria-selected:text-foreground"
          onClick={() => onSelect(f)}
        >
          {f}
        </button>
      ))}
      {children}
    </div>
  );
}

/** A note after the file tabs; the first one pushes the notes to the right. */
function FileNote({ className, ...props }: ComponentProps<"span">) {
  return <span className={cn("self-center px-1.5 text-xs whitespace-nowrap text-faint first-of-type:ml-auto", className)} {...props} />;
}

/** Says when completions aren't available yet, or why they failed. */
function LspNote({ status }: { status: LanguageStatus }) {
  if (status === "starting") return <FileNote>Starting completions…</FileNote>;
  if (typeof status === "object") return <FileNote title={status.error}>No completions</FileNote>;
  return null;
}

type ResultsProps = {
  detail: TaskDetail;
  run: Run | null;
  running: "run" | "submit" | null;
  error: string;
  solved: boolean;
  nextTask: TaskInfo | null;
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

const STATES: Record<string, { label: string; tone: string }> = {
  passed: { label: "Passed", tone: "text-pass" },
  failed: { label: "Failed", tone: "text-fail" },
  error: { label: "Error", tone: "text-fail" },
};

function Results(p: ResultsProps) {
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
