import { cn } from "cn";
import { useRef, type KeyboardEvent, type PointerEvent } from "react";

/**
 * A drag handle between two panes. It reports the pointer position along its
 * axis; the parent turns that into a size. Arrow keys nudge it, and a
 * double-click can reset it.
 *
 * Not shadcn's Resizable (react-resizable-panels), which lays the panes out
 * itself: these sit in CSS grids, the chat panel folds into its rail in the
 * same drag, and a container query stacks the exercise's panes when narrow.
 */
export function Splitter({
  axis,
  label,
  className,
  onDrag,
  onStep,
  onReset,
}: {
  axis: "x" | "y";
  label: string;
  className?: string;
  onDrag: (pos: number) => void;
  onStep: (dir: -1 | 1) => void;
  onReset?: () => void;
}) {
  const dragging = useRef(false);
  const pos = (e: PointerEvent) => (axis === "x" ? e.clientX : e.clientY);
  const end = (e: PointerEvent<HTMLDivElement>) => {
    dragging.current = false;
    document.body.classList.remove(`is-resizing-${axis}`);
    if (e.currentTarget.hasPointerCapture?.(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
  };
  return (
    <div
      className={cn(
        "relative z-2 flex-none bg-border transition-colors outline-none after:absolute after:content-[''] hover:bg-foreground focus-visible:bg-foreground",
        axis === "x" ? "w-px cursor-col-resize after:-inset-x-1 after:inset-y-0" : "h-px cursor-row-resize after:inset-x-0 after:-inset-y-1",
        className,
      )}
      role="separator"
      aria-orientation={axis === "x" ? "vertical" : "horizontal"}
      aria-label={label}
      tabIndex={0}
      onPointerDown={(e) => {
        e.preventDefault();
        dragging.current = true;
        document.body.classList.add(`is-resizing-${axis}`);
        e.currentTarget.setPointerCapture?.(e.pointerId);
      }}
      onPointerMove={(e) => dragging.current && onDrag(pos(e))}
      onPointerUp={end}
      onPointerCancel={end}
      onDoubleClick={onReset}
      onKeyDown={(e: KeyboardEvent) => {
        const back = axis === "x" ? "ArrowLeft" : "ArrowUp";
        const fwd = axis === "x" ? "ArrowRight" : "ArrowDown";
        if (e.key === back || e.key === fwd) {
          e.preventDefault();
          onStep(e.key === back ? -1 : 1);
        }
      }}
    />
  );
}
