import { useRef, useState, type DragEvent } from "react";
import { ImagePlus, X } from "lucide-react";
import type { Composer } from "@/features/chat/use-composer";
import { IMAGE_TYPES } from "@/features/chat/attachments";
import { AutoTextarea } from "@/components/auto-textarea";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type Props = {
  /** The draft and its images, kept above the panel (use-composer.ts). */
  composer: Composer;
  placeholder: string;
  onSend: (text: string, images: string[]) => void;
};

/** Where the learner writes to the tutor. Images can be pasted, dropped on it or picked with its attach button. */
export function MessageBox({ composer, placeholder, onSend }: Props) {
  const { draft, setDraft, images, imageError, attaching, inputRef, addImages, removeImage, take } = composer;
  const pickerRef = useRef<HTMLInputElement>(null);
  const [dropping, setDropping] = useState(false);
  const canSend = !attaching && (draft.trim() !== "" || images.length > 0);

  function send() {
    if (!canSend) return;
    const message = take();
    onSend(message.text, message.images);
  }

  function onDragOver(e: DragEvent) {
    if (!e.dataTransfer.types.includes("Files")) return;
    e.preventDefault();
    setDropping(true);
  }

  return (
    <>
      {imageError && (
        <p role="alert" className="mx-4 mb-1.5 flex-none text-xs text-fail">
          {imageError}
        </p>
      )}
      <div
        className={cn(
          "mx-3 mb-1 flex flex-none flex-col rounded-xl border border-input transition-colors focus-within:border-foreground",
          dropping && "border-foreground bg-muted",
        )}
        onDragOver={onDragOver}
        onDragLeave={(e) => !e.currentTarget.contains(e.relatedTarget as Node | null) && setDropping(false)}
        onDrop={(e) => {
          if (!e.dataTransfer.types.includes("Files")) return;
          e.preventDefault();
          setDropping(false);
          void addImages(Array.from(e.dataTransfer.files));
        }}
      >
        {images.length > 0 && (
          <ul className="flex flex-wrap gap-2 px-3 pt-2.5" aria-label="Attached images">
            {images.map((image) => (
              <li key={image.id} className="relative">
                <img src={image.url} alt={image.name || "Pasted image"} className="size-14 rounded-md border object-cover" />
                <Button
                  variant="outline"
                  className="absolute -top-1.5 -right-1.5 size-[18px] rounded-full p-0 text-muted-foreground hover:not-disabled:text-foreground"
                  aria-label={`Remove ${image.name || "the pasted image"}`}
                  title="Remove"
                  onClick={() => removeImage(image.id)}
                >
                  <X className="size-[11px]" />
                </Button>
              </li>
            ))}
          </ul>
        )}
        <div className="flex items-end gap-1 py-2 pr-2 pl-3">
          <AutoTextarea
            ref={inputRef}
            className="flex-1 rounded-none border-0 bg-transparent px-0 py-1 text-[13px] leading-[1.5] focus-visible:border-0"
            maxHeight={180}
            value={draft}
            placeholder={placeholder}
            aria-label="Message the tutor"
            onChange={(e) => setDraft(e.target.value)}
            onPaste={(e) => {
              const files = Array.from(e.clipboardData.files);
              if (files.length === 0) return; // plain text pastes as usual
              e.preventDefault();
              void addImages(files);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                send();
              }
            }}
          />
          <IconButton label="Attach images (or paste or drop them)" onClick={() => pickerRef.current?.click()}>
            <ImagePlus size={16} />
          </IconButton>
          <input
            ref={pickerRef}
            type="file"
            accept={IMAGE_TYPES.join(",")}
            multiple
            hidden
            onChange={(e) => {
              void addImages(Array.from(e.target.files ?? []));
              e.target.value = ""; // so the same file can be picked again
            }}
          />
          <Button
            size="icon-sm"
            className="ml-1 font-normal hover:not-disabled:border-primary hover:not-disabled:bg-primary disabled:border-input disabled:bg-input disabled:opacity-100"
            onClick={send}
            disabled={!canSend}
            aria-label="Send"
            title={attaching ? "Attaching images…" : "Send (Enter)"}
          >
            ↑
          </Button>
        </div>
      </div>
    </>
  );
}
