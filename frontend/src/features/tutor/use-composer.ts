import { useRef, useState } from "react";

/**
 * The tutor's message box. It lives above the panel, so a draft survives
 * folding the panel into its rail, and other pages can start a message.
 */
export function useComposer() {
  const [draft, setDraft] = useState("");
  const inputRef = useRef<HTMLTextAreaElement>(null);

  /** Starts a message with text, unless one is being written, with the cursor at its end. Open the panel first. */
  function prefill(text: string) {
    setDraft((d) => (d.trim() ? d : text));
    // The box mounts with the panel, so wait a frame for it.
    requestAnimationFrame(() => {
      const input = inputRef.current;
      input?.focus();
      input?.setSelectionRange(input.value.length, input.value.length);
    });
  }

  return { draft, setDraft, inputRef, prefill };
}

export type Composer = ReturnType<typeof useComposer>;
