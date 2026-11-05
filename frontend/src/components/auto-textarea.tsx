import { useLayoutEffect, useRef, type Ref, type TextareaHTMLAttributes } from "react";
import { Textarea } from "@/components/ui/textarea";

/**
 * A textarea that grows with its content up to maxHeight. Measured here, not
 * with CSS field-sizing, which not every WebKit the desktop app runs on has.
 */
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
    <Textarea
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
