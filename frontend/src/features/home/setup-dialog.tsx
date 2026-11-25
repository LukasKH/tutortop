import { useState, type ReactNode } from "react";
import { errorText, type Environment } from "@/lib/api";
import { checkEnvironment, hasAgent, useCheckingEnvironment } from "@/lib/agents";
import { ErrorText } from "@/components/error-text";
import { Link } from "@/components/markdown";
import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";

/**
 * A popup listing what Tutortop needs and can't find, and how to get it.
 * Check again looks once more; once nothing is missing, the caller stops
 * showing it.
 */
export function SetupDialog({ env, onClose }: { env: Environment; onClose: () => void }) {
  const checking = useCheckingEnvironment();
  const [error, setError] = useState("");
  // "Claude Code, Codex, or Antigravity": every agent the backend offers.
  const agents = new Intl.ListFormat("en", { type: "disjunction" }).format((env.providers ?? []).map((p) => p.name));

  return (
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent className="w-[min(480px,calc(100vw-48px))]">
        <AlertDialogHeader>
          <AlertDialogTitle>Finish setting up Tutortop</AlertDialogTitle>
          <AlertDialogDescription>Some things Tutortop needs aren't installed yet.</AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="flex flex-col gap-3">
          {!hasAgent(env) && <Missing title="A tutor">Your tutor runs on an AI coding agent. Install {agents}, sign in, then restart Tutortop.</Missing>}
          {!env.sandboxOk && (
            <Missing title="Docker or Podman">
              Checks run your code in a container, with <Link href="https://docs.docker.com/get-docker/">Docker</Link> or Podman.
              {env.sandboxHint && (
                <details className="mt-1.5">
                  <summary className="w-fit cursor-pointer text-xs text-muted-foreground hover:text-foreground">Details</summary>
                  <code className="mt-1 block text-xs whitespace-pre-wrap">{env.sandboxHint}</code>
                </details>
              )}
            </Missing>
          )}
        </ul>
        <ErrorText error={error} />
        <AlertDialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={checking}
            onClick={() => {
              setError("");
              checkEnvironment().catch((e) => setError(errorText(e)));
            }}
          >
            {checking ? "Checking…" : "Check again"}
          </Button>
          <Button type="button" onClick={onClose}>
            Continue
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function Missing({ title, children }: { title: string; children: ReactNode }) {
  return (
    <li className="flex flex-col gap-1 rounded-lg border px-3.5 py-3">
      <span className="text-sm font-[650]">{title}</span>
      <div className="text-sm text-soft-foreground [&_a]:text-foreground [&_a]:underline [&_a]:underline-offset-3">{children}</div>
    </li>
  );
}
