import type { ReactNode } from "react";
import { ErrorText } from "@/components/error-text";

/** A centred loading line, or an error with a way out. */
export function Placeholder({ error, loading, children }: { error: string; loading: string; children?: ReactNode }) {
  return (
    <div className="grid h-full place-items-center">
      {error ? (
        <div className="flex flex-col items-center gap-3.5">
          <ErrorText error={error} />
          {children}
        </div>
      ) : (
        <span className="text-muted-foreground">{loading}</span>
      )}
    </div>
  );
}
