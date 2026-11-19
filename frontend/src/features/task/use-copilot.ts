import { useEffect, useState } from "react";
import { CopilotService, errorText, type Suggestion } from "@/lib/api";
import { monaco } from "@/lib/monaco";
import { toPosition, toRange } from "@/features/task/lsp";

/** A suggestion as Monaco shows it, with Copilot's own for the notices back. */
type CopilotItem = monaco.languages.InlineCompletion & { suggestion: Suggestion };

/** Monaco runs this on accepting a suggestion (InlineCompletion.command). */
const ACCEPTED = "tutortop.copilot.accepted";

/**
 * GitHub Copilot's suggestions as grey ghost text in the learner's files,
 * while enabled in Settings. The backend cuts each to the rest of the line.
 * Like GitHub's own plugins, it tells Copilot which file is in front and which
 * suggestions were shown and accepted, and closes the files as the task does.
 * Returns what Copilot last said is wrong, such as a used-up allowance, or "".
 */
export function useCopilot(taskId: string, language: string, enabled: boolean, editor: monaco.editor.IStandaloneCodeEditor | null): string {
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (!enabled) return;
    const workPath = `/${taskId}/work/`;
    const command = monaco.editor.registerCommand(ACCEPTED, (_accessor, sg: Suggestion) => void CopilotService.Accepted(sg).catch(() => {}));
    const provider = monaco.languages.registerInlineCompletionsProvider(language, {
      // Ask once typing pauses, not on every key: each request counts
      // towards Copilot Free's monthly allowance.
      debounceDelayMs: 250,
      provideInlineCompletions: async (model, position, _context, token) => {
        if (!model.uri.path.startsWith(workPath)) return;
        const { tabSize, insertSpaces } = model.getOptions();
        const call = CopilotService.Complete({
          uri: model.uri.toString(),
          languageId: model.getLanguageId(),
          text: model.getValue(),
          position: toPosition(position),
          tabSize,
          insertSpaces,
        });
        token.onCancellationRequested(() => call.cancel());
        try {
          const res = await call;
          setNotice(res.notice ?? "");
          const items: CopilotItem[] = (res.suggestions ?? []).map((s) => ({
            insertText: s.text,
            range: toRange(s.range),
            command: { id: ACCEPTED, title: "Accept", arguments: [s] },
            suggestion: s,
          }));
          return { items };
        } catch (e) {
          if (!token.isCancellationRequested) setNotice(errorText(e));
          return;
        }
      },
      handleItemDidShow: (_completions, item) => void CopilotService.Shown((item as CopilotItem).suggestion).catch(() => {}),
      disposeInlineCompletions: () => {},
    });
    return () => {
      provider.dispose();
      command.dispose();
      CopilotService.CloseFiles().catch(() => {});
      setNotice("");
    };
  }, [taskId, language, enabled]);

  // The file in front: Copilot weighs it, and its neighbours, for context.
  useEffect(() => {
    if (!enabled || !editor) return;
    const focus = (uri: monaco.Uri | null) => uri?.path.startsWith(`/${taskId}/work/`) && CopilotService.Focus(uri.toString()).catch(() => {});
    focus(editor.getModel()?.uri ?? null);
    const listener = editor.onDidChangeModel((e) => focus(e.newModelUrl));
    return () => listener.dispose();
  }, [taskId, enabled, editor]);

  return notice;
}
