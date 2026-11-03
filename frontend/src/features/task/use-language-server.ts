import { useEffect, useState } from "react";
import { LanguageClient, type LanguageStatus } from "@/features/task/lsp";

/** A language server for completions, hover docs and errors while a task is open, if its language has one. */
export function useLanguageServer(wsId: string, taskId: string, language: string, enabled: boolean): LanguageStatus {
  const [status, setStatus] = useState<LanguageStatus>("off");
  useEffect(() => {
    if (!enabled) return;
    const client = new LanguageClient(wsId, taskId, language, setStatus);
    client.start();
    return () => {
      client.dispose();
      setStatus("off");
    };
  }, [wsId, taskId, language, enabled]);
  return status;
}
