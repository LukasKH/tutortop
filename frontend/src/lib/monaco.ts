// Monaco runs from the bundled package, never a CDN: this is a desktop app
// that may be offline. Only the base editor worker is needed: language
// smarts come from language servers in the sandbox (see lsp.ts).
import * as monaco from "monaco-editor";
import { loader } from "@monaco-editor/react";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

// "ink": quiet syntax colours on black. Soft hues so code is easy to scan
// without shouting: keywords lavender, types cyan, strings green, numbers
// peach, comments grey italic. Check results stay the only red and green UI.
monaco.editor.defineTheme("ink", {
  base: "vs-dark",
  inherit: false,
  rules: [
    { token: "", foreground: "e6e6e6", background: "111111" },
    { token: "keyword", foreground: "c4a7e7" },
    { token: "type", foreground: "7dcfff" },
    { token: "type.identifier", foreground: "7dcfff" },
    { token: "predefined", foreground: "89b4fa" },
    { token: "string", foreground: "a6d189" },
    { token: "string.escape", foreground: "e5c890" },
    { token: "number", foreground: "f5a97f" },
    { token: "regexp", foreground: "f4b8e4" },
    { token: "annotation", foreground: "e5c890" },
    { token: "delimiter", foreground: "9a9a9a" },
    { token: "operator", foreground: "9a9a9a" },
    { token: "comment", foreground: "6c7086", fontStyle: "italic" },
  ],
  colors: {
    "editor.background": "#111111",
    "editor.foreground": "#e6e6e6",
    "editorLineNumber.foreground": "#3f3f3f",
    "editorLineNumber.activeForeground": "#858585",
    "editor.lineHighlightBackground": "#161616",
    "editor.lineHighlightBorder": "#00000000",
    "editor.selectionBackground": "#ffffff2e",
    "editor.inactiveSelectionBackground": "#ffffff1a",
    "editorCursor.foreground": "#ededed",
    "editorWhitespace.foreground": "#2d2d2d",
    "editorIndentGuide.background1": "#1f1f1f",
    "editorIndentGuide.activeBackground1": "#2d2d2d",
    "editorBracketMatch.background": "#ffffff14",
    "editorBracketMatch.border": "#3f3f3f",
    // Brackets stay plain: no rainbow pairs.
    ...Object.fromEntries([1, 2, 3, 4, 5, 6].map((n) => [`editorBracketHighlight.foreground${n}`, "#e6e6e6"])),
    "editorBracketHighlight.unexpectedBracket.foreground": "#e5684f",
    "editorWidget.background": "#161616",
    "editorWidget.foreground": "#e6e6e6",
    "editorWidget.border": "#2d2d2d",
    "editorSuggestWidget.background": "#161616",
    "editorSuggestWidget.border": "#2d2d2d",
    "editorSuggestWidget.foreground": "#b8b8b8",
    "editorSuggestWidget.highlightForeground": "#ffffff",
    "editorSuggestWidget.selectedBackground": "#242424",
    "editorSuggestWidget.selectedForeground": "#ffffff",
    "editorHoverWidget.background": "#161616",
    "editorHoverWidget.border": "#2d2d2d",
    "editorGhostText.foreground": "#5a5a5a",
    "editorError.foreground": "#e5684f",
    "editorWarning.foreground": "#b8b8b8",
    "editorGutter.background": "#111111",
    "scrollbarSlider.background": "#ffffff1a",
    "scrollbarSlider.hoverBackground": "#ffffff2a",
    "scrollbarSlider.activeBackground": "#ffffff3a",
  },
});

loader.config({ monaco });
// Set now, not only when an editor opens: code in lessons is coloured with
// this theme's classes too (components/markdown.tsx).
monaco.editor.setTheme("ink");

// Monaco measures glyph widths once; measure again when the bundled mono font
// has loaded, or the cursor drifts from the text.
document.fonts.ready.then(() => monaco.editor.remeasureFonts());

export { monaco };

export const editorOptions: monaco.editor.IStandaloneEditorConstructionOptions = {
  fontFamily: '"Geist Mono Variable", ui-monospace, monospace',
  fontSize: 12.5,
  lineHeight: 22,
  fontLigatures: false,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  padding: { top: 14, bottom: 14 },
  renderLineHighlight: "line",
  bracketPairColorization: { enabled: false },
  guides: { bracketPairs: false },
  smoothScrolling: true,
  tabSize: 4,
  automaticLayout: true,
  overviewRulerLanes: 0,
  hideCursorInOverviewRuler: true,
  scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
  fixedOverflowWidgets: true,
  // Show the highlighted completion inline as grey ghost text; Tab or Enter accepts it.
  suggest: { preview: true, previewMode: "subwordSmart", showStatusBar: true },
  inlineSuggest: { enabled: true },
};
