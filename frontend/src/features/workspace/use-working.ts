import { createContext, use, useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { AppEvents, useEvent, type Working } from "@/lib/api";

type Listener = () => void;

const noopUnsubscribe = () => {};

export class WorkingStore {
  private byThread = new Map<string, Working | undefined>();
  private listeners = new Map<string, Set<Listener>>();

  get(thread: string): Working | undefined {
    return this.byThread.get(thread);
  }

  set(thread: string, value: Working | undefined) {
    if (this.byThread.get(thread) === value) return;
    this.byThread.set(thread, value);
    this.listeners.get(thread)?.forEach((fn) => fn());
  }

  clearAll() {
    const threads = [...this.byThread.keys()];
    this.byThread.clear();
    for (const t of threads) this.listeners.get(t)?.forEach((fn) => fn());
  }

  subscribe(thread: string, fn: Listener): () => void {
    let set = this.listeners.get(thread);
    if (!set) {
      set = new Set();
      this.listeners.set(thread, set);
    }
    set.add(fn);
    return () => {
      set.delete(fn);
      if (set.size === 0) this.listeners.delete(thread);
    };
  }
}

export const WorkingContext = createContext<WorkingStore | null>(null);

/**
 * Keeps each thread's live working state (reasoning or a tool call being
 * written) outside React state so per-token updates from background writers
 * only re-render the one component watching that thread.
 */
export function useWorkingStore(id: string): WorkingStore {
  const [store] = useState(() => new WorkingStore());

  useEffect(() => {
    store.clearAll();
  }, [id, store]);

  useEvent(AppEvents.ThreadWorking, (w) => {
    if (w.workspace === id) store.set(w.thread, w);
  });

  useEvent(AppEvents.ThreadBusy, ({ workspace, thread }) => {
    if (workspace === id) store.set(thread, undefined);
  });

  return store;
}

/** What one thread's agent is working on right now, if anything. */
export function useWorking(thread: string): Working | undefined {
  const store = use(WorkingContext);
  const subscribe = useCallback((cb: Listener) => (store ? store.subscribe(thread, cb) : noopUnsubscribe), [store, thread]);
  const getSnapshot = useCallback(() => store?.get(thread), [store, thread]);
  return useSyncExternalStore(subscribe, getSnapshot);
}
