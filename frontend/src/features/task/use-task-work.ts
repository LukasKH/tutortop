import { useEffect, useEffectEvent, useRef, useState } from "react";
import { App, errorText, type Result, type TaskDetail } from "@/lib/api";

/** A check of the learner's code: the visible tests (run), or all of them (submit). */
export type Run = { result: Result; submit: boolean };

/**
 * The learner's work on a task: it loads the task, saves edits shortly after
 * typing stops (and when the task closes), runs and submits the checks, and
 * resets the code to the starter.
 */
export function useTaskWork(wsId: string, taskId: string) {
  const runKey = `${wsId}/${taskId}`;
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const [files, setFiles] = useState<Record<string, string>>({});
  const [run, setRun] = useState<Run | null>(lastRuns.get(runKey) ?? null);
  const [running, setRunning] = useState<"run" | "submit" | null>(null);
  const [dirty, setDirty] = useState(false); // edits not yet saved
  const [error, setError] = useState("");

  // filesRef mirrors files because Monaco's onChange can run with a stale closure.
  const filesRef = useRef(files);
  const unsaved = useRef<Record<string, string> | null>(null);
  const saveTimer = useRef<number | undefined>(undefined);
  const runningRef = useRef(false);

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

  /** The learner changed a file; it saves once they pause. */
  function edit(name: string, value: string) {
    replaceFiles({ ...filesRef.current, [name]: value });
    unsaved.current = filesRef.current;
    setDirty(true);
    window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(save, 800);
  }

  /** Runs the visible tests, or all of them on submit. False if a check was already running. */
  async function check(submit: boolean): Promise<boolean> {
    if (runningRef.current || !detail) return false;
    runningRef.current = true;
    setRunning(submit ? "submit" : "run");
    setError("");
    cancelSave(); // RunChecks saves the files itself.
    try {
      const result = await App.RunChecks(wsId, taskId, filesRef.current, submit);
      const r = { result, submit };
      lastRuns.set(runKey, r);
      setRun(r);
      setDirty(false);
      if (submit && result.status === "passed") setDetail((d) => d && { ...d, passed: true });
    } catch (e) {
      setError(errorText(e));
      unsaved.current = filesRef.current;
      save();
    } finally {
      runningRef.current = false;
      setRunning(null);
    }
    return true;
  }

  /** Replaces the learner's code with the starter code. */
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

  // Load the task, and flush unsaved edits when leaving it.
  const loaded = useEffectEvent((d: TaskDetail) => {
    setDetail(d);
    replaceFiles(workOf(d));
  });
  const leave = useEffectEvent(() => {
    window.clearTimeout(saveTimer.current);
    save();
  });
  useEffect(() => {
    App.Task(wsId, taskId)
      .then(loaded)
      .catch((e) => setError(errorText(e)));
    return () => leave();
  }, [wsId, taskId]);

  return { detail, files, dirty, error, run, running, edit, check, reset };
}

// The latest check per task, so switching tasks doesn't lose it.
const lastRuns = new Map<string, Run>();

/** The learner's current code for each editable file. */
const workOf = (d: TaskDetail) => Object.fromEntries((d.files ?? []).map((f) => [f, d.work?.[f] ?? ""]));
