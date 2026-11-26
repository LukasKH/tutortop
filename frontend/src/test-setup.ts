import { afterAll } from "vitest";

// @wailsio/runtime/dist/drag.js starts a 50ms window.setInterval on import even
// after its handlers are attached. If a jsdom test file finishes faster than
// 50ms, Vitest tears down window before the callback runs and its
// window.clearInterval call throws ReferenceError. Clear any remaining window
// intervals before teardown.
if (typeof window !== "undefined") {
  const intervals = new Set<ReturnType<typeof setInterval>>();
  const realSet = window.setInterval.bind(window);
  const realClear = window.clearInterval.bind(window);
  window.setInterval = ((...args: Parameters<typeof setInterval>) => {
    const id = realSet(...args);
    intervals.add(id);
    return id;
  }) as typeof window.setInterval;
  window.clearInterval = ((id?: ReturnType<typeof setInterval>) => {
    if (id !== undefined) intervals.delete(id);
    realClear(id);
  }) as typeof window.clearInterval;
  afterAll(() => {
    for (const id of intervals) realClear(id);
    intervals.clear();
  });
}
