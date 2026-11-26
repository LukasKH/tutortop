import { useEffect, useEffectEvent, useRef, useState, type ComponentProps, type ReactNode } from "react";
import Editor from "@monaco-editor/react";
import { cn } from "@/lib/utils";
import { plural } from "@/lib/format";
import { clamp } from "@/lib/math";
import { editorOptions, monaco } from "@/lib/monaco";
import { modelPath, type LanguageStatus } from "@/features/task/lsp";
import { Results } from "@/features/task/results";
import { useLanguageServer } from "@/features/task/use-language-server";
import { useCopilot } from "@/features/task/use-copilot";
import { useSettings } from "@/features/settings/use-settings";
import { useTaskWork } from "@/features/task/use-task-work";
import { useLiveWorkspace } from "@/features/workspace/use-workspace";
import { HowWasThis } from "@/features/feedback/how-was-this";
import { TaskService } from "@/lib/api";
import { Markdown } from "@/components/markdown";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Page, PageHeader } from "@/components/page";
import { Placeholder } from "@/components/placeholder";
import { Splitter } from "@/components/splitter";
import { useStored } from "@/hooks/use-stored";

type Props = {
  taskId: string;
  /** From the workspace state; kept current by "changed" events. */
  passed: boolean;
  /** The module the task's objective belongs to, for the task bar. */
  moduleTitle: string;
  /** Shown beside the check buttons once solved: the way on (NextExercise). */
  next: ReactNode;
};

/** An exercise: the statement and tests beside the learner's editor, and the check results under it. */
export function TaskPanel({ taskId, passed, moduleTitle, next }: Props) {
  const { id: wsId } = useLiveWorkspace();
  const work = useTaskWork(wsId, taskId);
  const { detail, files, dirty, error, run, running } = work;
  const [activeFile, setActiveFile] = useState("");
  const [tab, setTab] = useState<"statement" | "tests">("statement");
  const [activeTest, setActiveTest] = useState("");
  const editorLanguage = detail?.editorLanguage || "plaintext";
  const lspStatus = useLanguageServer(wsId, taskId, editorLanguage, !!detail?.hasLanguageServer);
  const copilotOn = useSettings().settings?.copilot ?? false;

  // Layout the learner can adjust; remembered across tasks and restarts.
  const [split, setSplit] = useStored("split", 0.45); // statement's share of the width
  const [resultsOpen, setResultsOpen] = useStored("results.open", true);
  const [resultsHeight, setResultsHeight] = useStored("results.height", 220);
  const bodyRef = useRef<HTMLDivElement>(null);
  const workRef = useRef<HTMLElement>(null);

  async function check(submit: boolean) {
    if (await work.check(submit)) setResultsOpen(true);
  }

  // Keyboard: Ctrl/Cmd+Enter runs, with Shift it submits. The editor gets the
  // same keys as actions, since it swallows Ctrl+Enter itself.
  const [editor, setEditor] = useState<monaco.editor.IStandaloneCodeEditor | null>(null);
  const copilotNotice = useCopilot(taskId, editorLanguage, copilotOn, editor);
  const onCheckKey = useEffectEvent((submit: boolean) => void check(submit));
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.key !== "Enter" || !(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      onCheckKey(e.shiftKey);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    if (!editor) return;
    const actions = [
      editor.addAction({ id: "tutortop.run", label: "Run tests", keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter], run: () => onCheckKey(false) }),
      editor.addAction({
        id: "tutortop.submit",
        label: "Submit",
        keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.Enter],
        run: () => onCheckKey(true),
      }),
    ];
    return () => actions.forEach((a) => a.dispose());
  }, [editor]);

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
  // The first file and test file are open until the learner picks another.
  const fileShown = activeFile || editable[0] || "";
  const testShown = activeTest || testFiles[0] || "";
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
          className="min-h-0 min-w-[260px] flex-none @max-[640px]:h-2/5 @max-[640px]:w-auto! @max-[640px]:min-w-0 @max-[640px]:border-b"
          style={{ width: `${split * 100}%` }}
        >
          <TabsList aria-label="Exercise" className="px-[22px]">
            <TabsTrigger value="statement">Exercise</TabsTrigger>
            <TabsTrigger value="tests">Tests</TabsTrigger>
          </TabsList>
          <TabsContent value="statement" className="flex-1 overflow-auto text-base">
            <Markdown text={detail.statement} className="prose px-[26px] pt-[22px] pb-10" foldExamples lang={detail.editorLanguage} />
          </TabsContent>
          <TabsContent value="tests" className="flex min-h-0 flex-1 flex-col text-base">
            <FileTabs label="Test files" files={testFiles} active={testShown} onSelect={setActiveTest}>
              {detail.hiddenCount > 0 && <FileNote>+ {plural(detail.hiddenCount, "hidden test")} on submit</FileNote>}
            </FileTabs>
            <div className={EDITOR_HOST}>
              {testShown && (
                <Editor
                  path={modelPath(taskId, "tests", testShown)}
                  language={lang}
                  value={detail.tests?.[testShown] ?? ""}
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
          <FileTabs label="Your files" files={editable} active={fileShown} onSelect={setActiveFile}>
            <LspNote status={lspStatus} />
            {copilotNotice && (
              <FileNote className="max-w-64 truncate" title={copilotNotice}>
                Copilot: {copilotNotice}
              </FileNote>
            )}
            <FileNote>{dirty ? "Saving…" : "Saved"}</FileNote>
          </FileTabs>
          <div className={EDITOR_HOST}>
            {fileShown && (
              <Editor
                path={modelPath(taskId, "work", fileShown)}
                language={lang}
                value={files[fileShown] ?? ""}
                theme="ink"
                options={editorOptions}
                onChange={(v) => work.edit(fileShown, v ?? "")}
                onMount={setEditor}
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
            hiddenCount={detail.hiddenCount}
            run={run}
            running={running}
            error={error}
            solved={solved}
            next={next}
            feedback={<HowWasThis item={`task:${taskId}`} compact send={(fb) => TaskService.SendFeedback(wsId, taskId, fb)} />}
            open={resultsOpen}
            height={sizedResults ? resultsHeight : undefined}
            onToggle={() => setResultsOpen(!resultsOpen)}
            onReset={work.reset}
            onCheck={check}
          />
        </section>
      </div>
    </Page>
  );
}

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
    <Tabs value={active} onValueChange={onSelect}>
      <TabsList aria-label={label} className="gap-0 overflow-x-auto px-2.5">
        {files.map((f) => (
          <TabsTrigger key={f} value={f} className="px-3 font-mono text-xs font-normal">
            {f}
          </TabsTrigger>
        ))}
        {children}
      </TabsList>
    </Tabs>
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
