// Small shared widgets and hooks.
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  type Ref,
  type SetStateAction,
  type TextareaHTMLAttributes,
} from "react";
import { errorText } from "./api";

/** useState that survives reloads, stored in localStorage under "tutortop.<key>". */
export function useStored<T>(key: string, initial: T | (() => T)): [T, (v: SetStateAction<T>) => void] {
  const full = `tutortop.${key}`;
  const [value, setValue] = useState<T>(() => {
    const raw = localStorage.getItem(full);
    if (raw !== null) {
      try {
        return JSON.parse(raw) as T;
      } catch {
        /* fall through to the default */
      }
    }
    return typeof initial === "function" ? (initial as () => T)() : initial;
  });
  const set = (v: SetStateAction<T>) =>
    setValue((prev) => {
      const next = typeof v === "function" ? (v as (prev: T) => T)(prev) : v;
      localStorage.setItem(full, JSON.stringify(next));
      return next;
    });
  return [value, set];
}

/**
 * State for a submit button. run() marks it pending and clears the error; a
 * failure shows the error and re-enables it. Success leaves it pending on
 * purpose: the next view replaces the form, and the button must not be
 * clickable again while the app catches up.
 */
export function usePending() {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function run(action: () => Promise<unknown>) {
    setPending(true);
    setError("");
    try {
      await action();
    } catch (e) {
      setError(errorText(e));
      setPending(false);
    }
  }
  return { pending, error, setError, run };
}

/** A centred loading line, or an error with a way out. */
export function Placeholder({ error, loading, children }: { error: string; loading: string; children?: ReactNode }) {
  return (
    <div className="placeholder">
      {error ? (
        <div className="empty">
          <ErrorText error={error} />
          {children}
        </div>
      ) : (
        <span className="muted">{loading}</span>
      )}
    </div>
  );
}

/** An error message, announced when it appears; nothing without one. */
export function ErrorText({ error, className = "" }: { error: string; className?: string }) {
  if (!error) return null;
  return (
    <p className={`error-text ${className}`} role="alert">
      {error}
    </p>
  );
}

/** An icon-only button; its label is both the tooltip and the accessible name. */
export function IconButton({ label, className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return <button className={`icon-btn ${className}`} title={label} aria-label={label} {...props} />;
}

export function ProgressBar({ passed, total }: { passed: number; total: number }) {
  const pct = total > 0 ? Math.round((passed / total) * 100) : 0;
  return (
    <span className="progress" role="progressbar" aria-valuenow={passed} aria-valuemin={0} aria-valuemax={total}>
      <span className="progress-fill" style={{ width: `${pct}%` }} />
    </span>
  );
}

/**
 * A drag handle between two panes. It reports the pointer position along its
 * axis; the parent turns that into a size. Arrow keys nudge it, and a
 * double-click can reset it.
 */
export function Splitter({
  axis,
  label,
  className = "",
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
      className={`splitter splitter-${axis} ${className}`}
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

/** A textarea that grows with its content up to maxHeight. */
export function AutoTextarea({
  maxHeight = 200,
  ref,
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement> & { maxHeight?: number; ref?: Ref<HTMLTextAreaElement> }) {
  const own = useRef<HTMLTextAreaElement | null>(null);
  useLayoutEffect(() => {
    const el = own.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight + 2, maxHeight)}px`;
    el.style.overflowY = el.scrollHeight + 2 > maxHeight ? "auto" : "hidden";
  }, [props.value, maxHeight]);
  return (
    <textarea
      rows={1}
      {...props}
      ref={(el) => {
        own.current = el;
        if (typeof ref === "function") ref(el);
        else if (ref) ref.current = el;
      }}
    />
  );
}

export const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), Math.max(lo, hi));

/** A two-click confirmation that disarms itself after a few seconds. */
export function useArmed(): [boolean, () => void] {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const t = window.setTimeout(() => setArmed(false), 3500);
    return () => window.clearTimeout(t);
  }, [armed]);
  return [armed, () => setArmed(true)];
}

/**
 * A modal that asks before something that can't be undone. Cancel has focus,
 * so Enter doesn't confirm by accident; Escape or a click outside cancels.
 */
export function ConfirmDialog({
  title,
  children,
  confirmLabel,
  pendingLabel,
  onConfirm,
  onCancel,
}: {
  title: string;
  children: ReactNode;
  confirmLabel: string;
  pendingLabel: string;
  onConfirm: () => Promise<void>;
  onCancel: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const { pending, error, run } = usePending();

  useEffect(() => {
    ref.current?.showModal();
    cancelRef.current?.focus();
  }, []);

  const cancel = () => !pending && onCancel();
  return (
    <dialog
      ref={ref}
      className="confirm"
      aria-labelledby="confirm-title"
      onCancel={(e) => {
        e.preventDefault();
        cancel();
      }}
      onClick={(e) => e.target === e.currentTarget && cancel()}
      // Escape is handled here too: the dialog's own cancel event doesn't fire in every webview.
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          cancel();
        }
      }}
    >
      <h1 id="confirm-title">{title}</h1>
      <div className="confirm-body">{children}</div>
      <ErrorText error={error} />
      <div className="confirm-actions">
        <button ref={cancelRef} className="btn" onClick={cancel} disabled={pending}>
          Cancel
        </button>
        <button className="btn danger" onClick={() => run(onConfirm)} disabled={pending}>
          {pending ? pendingLabel : confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
