import { useRef, useState } from "react";
import { attach, MAX_IMAGES, type Attachment } from "@/features/chat/attachments";

/**
 * The tutor's message box. It lives above the panel, so a draft and its
 * images survive folding the panel into its rail, and other pages can start
 * a message.
 */
export function useComposer() {
  const [draft, setDraft] = useState("");
  const [images, setImages] = useState<Attachment[]>([]);
  /** Why the last images the learner added weren't all attached, or "". */
  const [imageError, setImageError] = useState("");
  /** How many picked, pasted or dropped batches are still being read. A message waits for them, or they'd land in the next one. */
  const [attaching, setAttaching] = useState(0);
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

  /** Attaches picked, pasted or dropped files, explaining any it refuses. */
  async function addImages(files: File[]) {
    if (files.length === 0) return;
    setAttaching((n) => n + 1);
    try {
      const { attached, errors } = await attach(files, MAX_IMAGES - images.length);
      // Two quick pastes both had room for the same slots.
      setImages((current) => [...current, ...attached].slice(0, MAX_IMAGES));
      setImageError(errors.join(" "));
    } finally {
      setAttaching((n) => n - 1);
    }
  }

  function removeImage(id: string) {
    setImages((current) => current.filter((i) => i.id !== id));
    setImageError("");
  }

  /** Empties the box once its message is sent, and returns what was in it. */
  function take() {
    const message = { text: draft.trim(), images: images.map((i) => i.data) };
    setDraft("");
    setImages([]);
    setImageError("");
    return message;
  }

  return { draft, setDraft, images, imageError, attaching: attaching > 0, inputRef, prefill, addImages, removeImage, take };
}

export type Composer = ReturnType<typeof useComposer>;
