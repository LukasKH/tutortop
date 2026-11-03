import { cn } from "cn";

/** An error message, announced when it appears; nothing without one. */
export function ErrorText({ error, className }: { error: string; className?: string }) {
  if (!error) return null;
  return (
    <p className={cn("text-sm whitespace-pre-wrap text-fail", className)} role="alert">
      {error}
    </p>
  );
}
