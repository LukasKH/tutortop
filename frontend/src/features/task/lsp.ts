// A small Language Server Protocol client for the task editor. The backend
// runs the server in the task's sandbox and relays raw JSON-RPC messages;
// this maps them onto Monaco: completions, hover, signature help, errors
// and formatting. Positions are UTF-16 in both, so only the base differs
// (LSP counts from 0, Monaco from 1).
import { Events } from "@wailsio/runtime";
import { App, AppEvents, errorText } from "@/lib/api";
import { monaco } from "@/lib/monaco";

type M = typeof monaco;
type Model = monaco.editor.ITextModel;
type Json = any; // eslint-disable-line @typescript-eslint/no-explicit-any

/** The Monaco model path of a task file. Work and test files both map to the task folder in the sandbox. */
export function modelPath(taskId: string, kind: "work" | "tests", file: string) {
  return `${taskId}/${kind}/${file}`;
}

export type LanguageStatus = "starting" | "ready" | "off" | { error: string };

type LspPosition = { line: number; character: number };
type LspRange = { start: LspPosition; end: LspPosition };

const toPosition = (p: monaco.IPosition): LspPosition => ({ line: p.lineNumber - 1, character: p.column - 1 });
const toRange = (r: LspRange): monaco.IRange => ({
  startLineNumber: r.start.line + 1,
  startColumn: r.start.character + 1,
  endLineNumber: r.end.line + 1,
  endColumn: r.end.character + 1,
});

/** LSP MarkupContent | MarkedString | MarkedString[] → Monaco markdown. */
function markdown(c: Json): monaco.IMarkdownString[] {
  if (c == null) return [];
  if (Array.isArray(c)) return c.flatMap(markdown);
  if (typeof c === "string") return c ? [{ value: c }] : [];
  if (c.kind === "plaintext") return [{ value: "```\n" + c.value + "\n```" }];
  if (c.language) return [{ value: "```" + c.language + "\n" + c.value + "\n```" }];
  return c.value ? [{ value: c.value }] : [];
}

function documentation(c: Json): string | monaco.IMarkdownString | undefined {
  if (c == null) return undefined;
  return typeof c === "string" ? c : markdown(c)[0];
}

const SEVERITY: Record<number, monaco.MarkerSeverity> = {
  1: monaco.MarkerSeverity.Error,
  2: monaco.MarkerSeverity.Warning,
  3: monaco.MarkerSeverity.Info,
  4: monaco.MarkerSeverity.Hint,
};

// LSP CompletionItemKind (1-based) → Monaco's.
const K = monaco.languages.CompletionItemKind;
const KINDS = [
  K.Text,
  K.Method,
  K.Function,
  K.Constructor,
  K.Field,
  K.Variable,
  K.Class,
  K.Interface,
  K.Module,
  K.Property,
  K.Unit,
  K.Value,
  K.Enum,
  K.Keyword,
  K.Snippet,
  K.Color,
  K.File,
  K.Reference,
  K.Folder,
  K.EnumMember,
  K.Constant,
  K.Struct,
  K.Event,
  K.Operator,
  K.TypeParameter,
];

const textEdits = (edits: Json[] | null | undefined): monaco.languages.TextEdit[] => (edits ?? []).map((e) => ({ range: toRange(e.range), text: e.newText }));

type Doc = { uri: string; version: number; listener: monaco.IDisposable };
type Pending = { resolve: (v: Json) => void; reject: (e: Error) => void };

export class LanguageClient {
  private id = "";
  private root = "";
  private nextId = 1;
  private pending = new Map<number, Pending>();
  private docs = new Map<string, Doc>(); // Monaco model uri → open document
  private disposables: monaco.IDisposable[] = [];
  private caps: Json = {};
  private closed = false;
  private unsubscribe?: () => void;

  constructor(
    private wsId: string,
    private taskId: string,
    private languageId: string,
    private onStatus: (s: LanguageStatus) => void,
  ) {}

  async start() {
    this.onStatus("starting");
    try {
      const info = await App.StartLanguageServer(this.wsId, this.taskId);
      if (this.closed) {
        if (info.id) App.StopLanguageServer(info.id);
        return;
      }
      if (!info.id) return this.onStatus("off");
      this.id = info.id;
      this.root = info.rootUri;
      this.unsubscribe = Events.On(AppEvents.LspMessage, ({ data }) => {
        if (data.id !== this.id) return;
        if (data.closed) this.stopped();
        else this.receive(JSON.parse(data.message));
      });
      const res = await this.request("initialize", {
        processId: null,
        clientInfo: { name: "Tutortop" },
        rootUri: this.root,
        workspaceFolders: [{ uri: this.root, name: "task" }],
        capabilities: CLIENT_CAPABILITIES,
      });
      if (this.closed) return;
      this.caps = res?.capabilities ?? {};
      this.notify("initialized", {});
      this.register(monaco);
      for (const m of monaco.editor.getModels()) this.open(m);
      this.disposables.push(monaco.editor.onDidCreateModel((m) => this.open(m)));
      this.onStatus("ready");
    } catch (e) {
      if (!this.closed) this.onStatus({ error: errorText(e) });
    }
  }

  dispose() {
    if (this.closed) return;
    this.closed = true;
    this.unsubscribe?.();
    for (const d of this.disposables) d.dispose();
    for (const [key, doc] of this.docs) {
      doc.listener.dispose();
      const model = monaco.editor.getModel(monaco.Uri.parse(key));
      if (model) monaco.editor.setModelMarkers(model, "lsp", []);
    }
    this.docs.clear();
    for (const p of this.pending.values()) p.reject(new Error("closed"));
    this.pending.clear();
    if (this.id) App.StopLanguageServer(this.id);
  }

  private stopped() {
    if (this.closed) return;
    this.dispose();
    this.onStatus({ error: "The language server stopped." });
  }

  // ---- JSON-RPC ----

  private send(msg: Json) {
    if (this.closed) return;
    App.SendLanguageServer(this.id, JSON.stringify({ jsonrpc: "2.0", ...msg })).catch(() => this.stopped());
  }

  private notify(method: string, params: Json) {
    this.send({ method, params });
  }

  private request(method: string, params: Json, token?: monaco.CancellationToken): Promise<Json> {
    const id = this.nextId++;
    token?.onCancellationRequested(() => this.notify("$/cancelRequest", { id }));
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.send({ id, method, params });
    });
  }

  private receive(msg: Json) {
    if (msg.method === undefined) {
      const p = this.pending.get(msg.id);
      this.pending.delete(msg.id);
      if (msg.error) p?.reject(new Error(msg.error.message));
      else p?.resolve(msg.result);
    } else if (msg.id !== undefined) {
      // Requests from the server (configuration, capability registration,
      // progress tokens): defaults are fine for all of them.
      const result = msg.method === "workspace/configuration" ? (msg.params?.items ?? []).map(() => null) : null;
      this.send({ id: msg.id, result });
    } else if (msg.method === "textDocument/publishDiagnostics") {
      this.diagnostics(msg.params);
    }
  }

  // ---- documents ----

  /** The server's uri for one of this task's models, or "" if it isn't one. */
  private uriOf(model: Model): string {
    for (const kind of ["work", "tests"] as const) {
      const prefix = `/${this.taskId}/${kind}/`;
      if (model.uri.path.startsWith(prefix)) return `${this.root}/${model.uri.path.slice(prefix.length)}`;
    }
    return "";
  }

  private open(model: Model) {
    const uri = this.uriOf(model);
    const key = model.uri.toString();
    if (!uri || this.docs.has(key)) return;
    const doc: Doc = {
      uri,
      version: 1,
      listener: model.onDidChangeContent(() => {
        // A change without a range replaces the whole text; fine for task-sized files.
        this.notify("textDocument/didChange", { textDocument: { uri, version: ++doc.version }, contentChanges: [{ text: model.getValue() }] });
      }),
    };
    model.onWillDispose(() => {
      if (this.docs.get(key) !== doc) return;
      doc.listener.dispose();
      this.docs.delete(key);
      this.notify("textDocument/didClose", { textDocument: { uri } });
    });
    this.docs.set(key, doc);
    this.notify("textDocument/didOpen", { textDocument: { uri, languageId: this.languageId, version: 1, text: model.getValue() } });
  }

  private modelFor(uri: string): Model | null {
    for (const [key, doc] of this.docs) if (doc.uri === uri) return monaco.editor.getModel(monaco.Uri.parse(key));
    return null;
  }

  private docOf(model: Model) {
    const doc = this.docs.get(model.uri.toString());
    return doc && { uri: doc.uri };
  }

  private diagnostics({ uri, diagnostics }: { uri: string; diagnostics: Json[] }) {
    const model = this.modelFor(uri);
    if (!model) return;
    monaco.editor.setModelMarkers(
      model,
      "lsp",
      diagnostics.map((d) => ({
        ...toRange(d.range),
        severity: SEVERITY[d.severity] ?? monaco.MarkerSeverity.Error,
        message: d.message,
        source: d.source,
        code: d.code == null ? undefined : String(d.code),
        tags: d.tags,
      })),
    );
  }

  // ---- editor features ----

  private register(m: M) {
    const lang = this.languageId;
    const caps = this.caps;
    const add = (d: monaco.IDisposable) => this.disposables.push(d);

    if (caps.completionProvider) {
      const originals = new WeakMap<monaco.languages.CompletionItem, Json>();
      add(
        m.languages.registerCompletionItemProvider(lang, {
          triggerCharacters: caps.completionProvider.triggerCharacters,
          provideCompletionItems: async (model, position, context, token) => {
            const textDocument = this.docOf(model);
            if (!textDocument) return undefined;
            const res = await this.request(
              "textDocument/completion",
              {
                textDocument,
                position: toPosition(position),
                context: { triggerKind: context.triggerKind + 1, triggerCharacter: context.triggerCharacter },
              },
              token,
            );
            const items: Json[] = Array.isArray(res) ? res : (res?.items ?? []);
            const defaults = res?.itemDefaults ?? {};
            const word = model.getWordUntilPosition(position);
            const wordRange = new m.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn);
            const suggestions = items.map((it) => {
              const s = this.completion(m, it, defaults, wordRange);
              originals.set(s, it);
              return s;
            });
            return { suggestions, incomplete: !!res?.isIncomplete };
          },
          resolveCompletionItem: caps.completionProvider.resolveProvider
            ? async (item, token) => {
                const original = originals.get(item);
                if (!original) return item;
                const r = await this.request("completionItem/resolve", original, token);
                if (!r) return item;
                return {
                  ...item,
                  detail: r.detail ?? item.detail,
                  documentation: documentation(r.documentation) ?? item.documentation,
                  additionalTextEdits: r.additionalTextEdits ? textEdits(r.additionalTextEdits) : item.additionalTextEdits,
                };
              }
            : undefined,
        }),
      );
    }

    if (caps.hoverProvider) {
      add(
        m.languages.registerHoverProvider(lang, {
          provideHover: async (model, position, token) => {
            const textDocument = this.docOf(model);
            if (!textDocument) return undefined;
            const r = await this.request("textDocument/hover", { textDocument, position: toPosition(position) }, token);
            const contents = markdown(r?.contents);
            if (!contents.length) return undefined;
            return { contents, range: r.range ? toRange(r.range) : undefined };
          },
        }),
      );
    }

    if (caps.signatureHelpProvider) {
      add(
        m.languages.registerSignatureHelpProvider(lang, {
          signatureHelpTriggerCharacters: caps.signatureHelpProvider.triggerCharacters,
          signatureHelpRetriggerCharacters: caps.signatureHelpProvider.retriggerCharacters,
          provideSignatureHelp: async (model, position, token, context) => {
            const textDocument = this.docOf(model);
            if (!textDocument) return undefined;
            const r = await this.request(
              "textDocument/signatureHelp",
              {
                textDocument,
                position: toPosition(position),
                context: { triggerKind: context.triggerKind, triggerCharacter: context.triggerCharacter, isRetrigger: context.isRetrigger },
              },
              token,
            );
            if (!r?.signatures?.length) return undefined;
            return {
              value: {
                activeSignature: r.activeSignature ?? 0,
                activeParameter: r.activeParameter ?? 0,
                signatures: r.signatures.map((s: Json) => ({
                  label: s.label,
                  documentation: documentation(s.documentation),
                  activeParameter: s.activeParameter,
                  parameters: (s.parameters ?? []).map((p: Json) => ({ label: p.label, documentation: documentation(p.documentation) })),
                })),
              },
              dispose() {},
            };
          },
        }),
      );
    }

    if (caps.documentFormattingProvider) {
      add(
        m.languages.registerDocumentFormattingEditProvider(lang, {
          provideDocumentFormattingEdits: async (model, options, token) => {
            const textDocument = this.docOf(model);
            if (!textDocument) return undefined;
            const r = await this.request(
              "textDocument/formatting",
              { textDocument, options: { tabSize: options.tabSize, insertSpaces: options.insertSpaces } },
              token,
            );
            return textEdits(r);
          },
        }),
      );
    }
  }

  private completion(m: M, it: Json, defaults: Json, wordRange: monaco.IRange): monaco.languages.CompletionItem {
    const edit = it.textEdit;
    const editRange = edit ? ("range" in edit ? edit.range : edit) : defaults.editRange;
    let range: monaco.languages.CompletionItem["range"] = wordRange;
    if (editRange?.insert) range = { insert: toRange(editRange.insert), replace: toRange(editRange.replace) };
    else if (editRange?.start) range = toRange(editRange);
    const snippet = (it.insertTextFormat ?? defaults.insertTextFormat) === 2;
    return {
      label: it.labelDetails ? { label: it.label, detail: it.labelDetails.detail, description: it.labelDetails.description } : it.label,
      kind: KINDS[(it.kind ?? 1) - 1] ?? K.Text,
      detail: it.detail,
      documentation: documentation(it.documentation),
      insertText: edit?.newText ?? it.textEditText ?? it.insertText ?? it.label,
      insertTextRules: snippet ? m.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
      range,
      sortText: it.sortText,
      filterText: it.filterText,
      preselect: it.preselect,
      commitCharacters: it.commitCharacters ?? defaults.commitCharacters,
      additionalTextEdits: it.additionalTextEdits ? textEdits(it.additionalTextEdits) : undefined,
      tags: it.tags?.includes(1) || it.deprecated ? [m.languages.CompletionItemTag.Deprecated] : undefined,
    };
  }
}

const markup = { documentationFormat: ["markdown", "plaintext"] };

const CLIENT_CAPABILITIES = {
  general: { positionEncodings: ["utf-16"] },
  textDocument: {
    synchronization: { dynamicRegistration: false },
    completion: {
      contextSupport: true,
      completionItem: {
        ...markup,
        snippetSupport: true,
        insertReplaceSupport: true,
        labelDetailsSupport: true,
        deprecatedSupport: true,
        tagSupport: { valueSet: [1] },
        resolveSupport: { properties: ["documentation", "detail", "additionalTextEdits"] },
      },
      completionList: { itemDefaults: ["commitCharacters", "editRange", "insertTextFormat"] },
    },
    hover: { contentFormat: ["markdown", "plaintext"] },
    signatureHelp: {
      contextSupport: true,
      signatureInformation: { ...markup, parameterInformation: { labelOffsetSupport: true }, activeParameterSupport: true },
    },
    publishDiagnostics: { tagSupport: { valueSet: [1, 2] } },
    formatting: {},
  },
  workspace: { configuration: true, workspaceFolders: true },
};
