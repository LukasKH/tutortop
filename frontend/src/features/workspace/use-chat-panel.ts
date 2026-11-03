import { clamp } from "@/components/splitter";
import { useStored } from "@/hooks/use-stored";

/**
 * The tutor panel beside the workspace: open, or folded into its rail, and its
 * width. Both are remembered. Dragging keeps it between CHAT_MIN and half the
 * window; dragging well below the minimum folds it.
 */
export function useChatPanel() {
  const [open, setOpen] = useStored("chat.open", true);
  const [width, setWidth] = useStored("chat.width", defaultWidth);

  // One bounded setter, shared by dragging and the keyboard.
  const size = (w: number) => setWidth(Math.round(clamp(w, CHAT_MIN, window.innerWidth / 2)));

  return {
    open,
    setOpen,
    width,
    /** The pointer is at x: the panel spans from there to the window's right edge. */
    drag(x: number) {
      const w = window.innerWidth - x;
      if (w < CHAT_FOLD) return open && setOpen(false);
      if (!open) setOpen(true);
      size(w);
    },
    /** An arrow key: -1 widens, 1 narrows; a folded panel opens. */
    step(dir: -1 | 1) {
      if (open) size(width - dir * 24);
      else setOpen(true);
    },
    reset() {
      setOpen(true);
      setWidth(defaultWidth());
    },
  };
}

const CHAT_MIN = 300;
const CHAT_FOLD = 220;
const defaultWidth = () => (window.innerWidth >= 1440 ? 380 : 340);
