import { Window } from "@wailsio/runtime";
import { clamp } from "@/lib/math";

// The desktop app's size. The webview doesn't zoom with Ctrl +/- by itself,
// so the app does, with the window's native zoom (like a browser's, so the
// editor and the splitters keep working). It also grows with the window:
// on a wide screen everything gets bigger instead of leaving empty space.
// Ctrl + / - / scroll adjust from there, and Ctrl 0 goes back to automatic.
// In a browser (server mode) the browser's own zoom does all this.

export async function startZoom() {
  if (!isDesktop) return;
  applied = (await Window.GetZoom().catch(() => 1)) || 1;
  apply(false);

  let resizing = 0;
  window.addEventListener("resize", () => {
    clearTimeout(resizing);
    resizing = window.setTimeout(() => apply(false), 150);
  });
  // Capture, so the code editor doesn't take the keys first.
  window.addEventListener(
    "keydown",
    (e) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
      const by = e.key === "=" || e.key === "+" ? STEP : e.key === "-" || e.key === "_" ? 1 / STEP : e.key === "0" ? null : undefined;
      if (by === undefined) return;
      e.preventDefault();
      e.stopPropagation();
      zoom(by);
    },
    true,
  );
  // Ctrl + scroll, and pinching on a touchpad. A notch or two per step.
  let pending = 0;
  window.addEventListener(
    "wheel",
    (e) => {
      if (!e.ctrlKey) return;
      e.preventDefault();
      pending += e.deltaY;
      if (Math.abs(pending) < 60) return;
      zoom(pending < 0 ? STEP : 1 / STEP);
      pending = 0;
    },
    { passive: false, capture: true },
  );
}

type Bridge = {
  chrome?: { webview?: unknown };
  webkit?: { messageHandlers?: { external?: unknown } };
};
const bridge = window as unknown as Bridge;
const isDesktop = !!(bridge.chrome?.webview || bridge.webkit?.messageHandlers?.external);

/** At this window width (at 100%) and below, the app is at 100%. */
const BASE_WIDTH = 1600;
const STEP = 1.1;
// The Linux webview can't zoom below 100%.
const MIN = navigator.userAgent.includes("Linux") ? 1 : 0.5;
const MAX = 3;
const KEY = "zoom.factor";

/** The learner's adjustment on top of the automatic size. */
let factor = Number(localStorage.getItem(KEY)) || 1;
/** The zoom the window has now. */
let applied = 1;

function zoom(by: number | null) {
  factor = by === null ? 1 : factor * by;
  apply(true);
}

function apply(announce: boolean) {
  const auto = automatic();
  const level = Math.round(clamp(auto * factor, MIN, MAX) * 100) / 100;
  // Pressing past a limit doesn't build up.
  factor = level / auto;
  localStorage.setItem(KEY, String(factor));
  if (announce) show(level, Math.abs(factor - 1) < 0.01);
  if (Math.abs(level - applied) < 0.005) return;
  applied = level;
  Window.SetZoom(level).catch(() => {});
}

/** The size for this window: 100% up to BASE_WIDTH, then growing with it. */
function automatic(): number {
  // innerWidth is in zoomed pixels; times the zoom it's the real width.
  const width = window.innerWidth * applied;
  return Math.round(clamp(width / BASE_WIDTH, 1, 2) * 20) / 20;
}

let toast: HTMLDivElement | null = null;
let hide = 0;
function show(level: number, auto: boolean) {
  if (!toast) {
    toast = document.createElement("div");
    toast.className =
      "pointer-events-none fixed top-4 left-1/2 z-100 -translate-x-1/2 rounded-lg border border-input bg-muted px-3 py-1.5 text-sm font-medium text-foreground tabular-nums opacity-0 transition-opacity duration-120 data-shown:opacity-100";
    toast.setAttribute("role", "status");
    document.body.append(toast);
  }
  toast.textContent = `${Math.round(level * 100)}%${auto ? " · automatic" : ""}`;
  toast.dataset.shown = "";
  clearTimeout(hide);
  hide = window.setTimeout(() => toast && delete toast.dataset.shown, 1200);
}
