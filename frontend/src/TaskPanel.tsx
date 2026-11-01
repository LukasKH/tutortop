import { useEffect, useRef, useState, type ReactNode } from "react";
import Editor from "@monaco-editor/react";
import { App, errorText, plural, type Result, type TaskDetail, type TaskInfo, type TestResult } from "./api";
import { editorOptions, monaco } from "./monaco";
import { LanguageClient, modelPath, type LanguageStatus } from "./lsp";
import { Markdown } from "./Markdown";
import { ChevronDown } from "./icons";
import { IconButton, Placeholder, Splitter, clamp, useArmed, useStored } from "./ui";

type Props = {
  wsId: string;
  taskId: string;
  /** From the workspace state; kept current by "changed" events. */
  passed: boolean;
  /** The module the task's objective belongs to, for the task bar. */
  moduleTitle: string;
  /** The first unsolved task after this one, if the coach has written it yet. */
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
    <div className="task">
      <header className="taskbar">
        <div className="crumbs truncate" title={detail.title}>
          {moduleTitle && <span className="muted">{moduleTitle} / </span>}
          <b>{detail.title}</b>
        </div>
      </header>

      <div className="task-body" ref={bodyRef}>
        <section className="brief" style={{ width: `${split * 100}%` }}>
          <div className="tabs" role="tablist" aria-label="Exercise">
            <button
              id="tab-statement"
              role="tab"
              aria-selected={tab === "statement"}
              aria-controls="panel-brief"
              className="tab"
              onClick={() => setTab("statement")}
            >
              Exercise
            </button>
            <button id="tab-tests" role="tab" aria-selected={tab === "tests"} aria-controls="panel-brief" className="tab" onClick={() => setTab("tests")}>
              Tests
            </button>
          </div>
          {tab === "statement" ? (
            <div className="brief-scroll" id="panel-brief" role="tabpanel" aria-labelledby="tab-statement">
              <Markdown text={detail.statement} className="prose statement" foldExamples lang={detail.editorLanguage} />
            </div>
          ) : (
            <div className="brief-tests" id="panel-brief" role="tabpanel" aria-labelledby="tab-tests">
              <FileTabs label="Test files" files={testFiles} active={activeTest} onSelect={setActiveTest}>
                {detail.hiddenCount > 0 && <span className="file-note">+ {plural(detail.hiddenCount, "hidden test")} on submit</span>}
              </FileTabs>
              <div className="editor-host">
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
            </div>
          )}
        </section>

        <Splitter
          axis="x"
          label="Resize statement and editor"
          onDrag={(x) => {
            const r = bodyRef.current!.getBoundingClientRect();
            sizeSplit((x - r.left) / r.width);
          }}
          onStep={(d) => sizeSplit(split + d * 0.03)}
        />

        <section className="work" ref={workRef}>
          <FileTabs label="Your files" files={editable} active={activeFile} onSelect={setActiveFile}>
            <LspNote status={lspStatus} />
            <span className="file-note">{dirty ? "Saving…" : "Saved"}</span>
          </FileTabs>
          <div className="editor-host">
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
    </div>
  );
}

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
    <div className="file-tabs" role="tablist" aria-label={label}>
      {files.map((f) => (
        <button key={f} role="tab" aria-selected={active === f} className="file-tab" onClick={() => onSelect(f)}>
          {f}
        </button>
      ))}
      {children}
    </div>
  );
}

/** Says when completions aren't available yet, or why they failed. */
function LspNote({ status }: { status: LanguageStatus }) {
  if (status === "starting") return <span className="file-note">Starting completions…</span>;
  if (typeof status === "object")
    return (
      <span className="file-note" title={status.error}>
        No completions
      </span>
    );
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
  passed: { label: "Passed", tone: "pass" },
  failed: { label: "Failed", tone: "fail" },
  error: { label: "Error", tone: "fail" },
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
    <div className="results" style={p.height ? { height: p.height } : undefined}>
      <div className="results-bar">
        {running ? (
          <span className="muted">{running === "submit" ? "Running all tests…" : "Running visible tests…"}</span>
        ) : state ? (
          <>
            <b className={state.tone}>{state.label}</b>
            <span className="num">
              {tests.length > 0 && `${passedCount}/${tests.length} tests · `}
              {(r!.durationMs / 1000).toFixed(1)}s{notRun > 0 && ` · ${notRun} not run`}
            </span>
          </>
        ) : (
          <span className="muted truncate" title="Run checks the visible tests. Submit runs all of them, including hidden ones.">
            {detail.hiddenCount > 0 ? `Submit adds ${plural(detail.hiddenCount, "hidden test")}` : ""}
          </span>
        )}
        <span className="spacer" />
        <button
          className={`btn text ${resetArmed ? "is-armed" : ""}`}
          onClick={() => (resetArmed ? p.onReset() : armReset())}
          disabled={!!running}
          title="Replace your code with the starter code"
        >
          {resetArmed ? "Discard your code?" : "Reset"}
        </button>
        <button className="btn" onClick={() => p.onCheck(false)} disabled={!!running} title="Run the visible tests">
          {running === "run" ? "Running…" : "Run"} <kbd>{KEY_RUN}</kbd>
        </button>
        {/* Once passed, Next is the main action and Submit steps back. */}
        <button className={`btn ${solved ? "" : "ink"}`} onClick={() => p.onCheck(true)} disabled={!!running} title="Run all tests, including hidden ones">
          {running === "submit" ? "Submitting…" : "Submit"} <kbd>{KEY_SUBMIT}</kbd>
        </button>
        {solved &&
          (p.nextTask ? (
            <button className="btn ink" onClick={() => p.onOpenTask(p.nextTask!.id)} title={p.nextTask.title}>
              Next exercise →
            </button>
          ) : p.moreComing ? (
            <span className="muted">Preparing next exercise…</span>
          ) : (
            <span className="muted">Plan complete</span>
          ))}
        <IconButton label={p.open ? "Hide results" : "Show results"} className="results-toggle" onClick={p.onToggle} aria-expanded={p.open}>
          <ChevronDown />
        </IconButton>
      </div>

      {p.open && error && (
        <p className="results-error" role="alert">
          {error}
        </p>
      )}

      {p.open && r && (
        <div className="results-body">
          {tests.length > 0 && (
            <ul className="tests">
              {tests.map((t, i) => {
                const row = (
                  <>
                    <TestMark test={t} />
                    <span className="test-name">{t.name}</span>
                    {t.notRun && <span className="muted">didn't run</span>}
                    {t.hidden && <span className="test-hidden">hidden</span>}
                  </>
                );
                // A failure's message folds out under its row; the first one starts open.
                const failed = !t.passed && !t.notRun && !!t.message;
                return (
                  <li key={`${t.hidden ? "h" : "v"}:${t.name}`} className={failed ? "test is-wide" : "test"}>
                    {failed ? (
                      <details open={i === firstFail}>
                        <summary className="test-row">{row}</summary>
                        <pre className="test-message">{t.message}</pre>
                      </details>
                    ) : (
                      <div className="test-row" title={t.notRun ? t.message : undefined}>
                        {row}
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          {r.output && (
            <details className="output" open={r.status === "error"}>
              <summary>Output</summary>
              <pre>{r.output}</pre>
            </details>
          )}
        </div>
      )}
    </div>
  );
}

function TestMark({ test }: { test: TestResult }) {
  if (test.notRun)
    return (
      <span className="test-mark muted" aria-label="Didn't run">
        –
      </span>
    );
  return (
    <span className={`test-mark ${test.passed ? "pass" : "fail"}`} aria-label={test.passed ? "Passed" : "Failed"}>
      {test.passed ? "✓" : "✗"}
    </span>
  );
}
