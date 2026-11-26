import { useEffect, useMemo, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import remarkParse from "remark-parse";
import { unified, type Pluggable } from "unified";
import { visit } from "unist-util-visit";
import type { Element, ElementContent, Root } from "hast";
import type { Root as MdastRoot } from "mdast";
import type { VFile } from "vfile";
import { Browser } from "@wailsio/runtime";
import { Diagram } from "@/components/diagram";
import { monaco } from "@/lib/monaco";

/**
 * Markdown as the app shows it. `lang` is the language of the surrounding
 * exercise or lesson: code without a language of its own is coloured as that.
 */
export function Markdown({ text, className, foldExamples, lang }: { text: string; className?: string; foldExamples?: boolean; lang?: string }) {
  const components = useMemo(
    () => ({
      a: Link,
      table: Table,
      // Fenced code: ```mermaid is drawn as a diagram, ```diff as a change,
      // ```text (trees, pseudocode) as it is, the rest coloured like the editor.
      pre: ({ node }: { node?: Element }) => {
        const code = node?.children[0];
        if (code?.type !== "element") return null;
        const fence = /language-(\S+)/.exec(String(code.properties.className ?? ""))?.[1];
        const source = textOf(code);
        if (fence === "mermaid") return <Diagram code={source.trim()} />;
        if (fence === "diff") return <DiffBlock code={source.replace(/\n$/, "")} />;
        return <CodeBlock code={source.replace(/\n$/, "")} lang={fence ?? lang} />;
      },
      // Only inline code reaches here: fenced code is drawn whole by pre.
      code: ({ children }: { children?: ReactNode }) => <InlineCode code={String(children ?? "")} lang={lang} />,
    }),
    [lang],
  );
  const source = useMemo(() => escapeDollarAmounts(text), [text]);
  return (
    <div className={className}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath, displayMathParagraphs]}
        rehypePlugins={foldExamples ? [katex, foldExampleCode] : [katex]}
        components={components}
      >
        {source}
      </ReactMarkdown>
    </div>
  );
}

// Links open in the system browser; navigating the app's webview away would
// lose the learner's session.
export function Link({ href, children }: { href?: string; children?: ReactNode }) {
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        if (href && /^https?:/i.test(href)) Browser.OpenURL(href);
      }}
    >
      {children}
    </a>
  );
}

// Wide tables scroll inside their own box instead of stretching the pane.
function Table({ children }: { children?: ReactNode }) {
  return (
    <div className="table-wrap">
      <table>{children}</table>
    </div>
  );
}

/**
 * A ```diff block: what changes between a before and an after. Added lines
 * are green, removed ones red, the context grey, so the change stands out.
 */
function DiffBlock({ code }: { code: string }) {
  return (
    <pre className="diff">
      <code>
        {code.split("\n").map((line, i) => (
          <span key={i} className={line.startsWith("+") ? "diff-add" : line.startsWith("-") ? "diff-del" : "diff-ctx"}>
            {line || " "}
          </span>
        ))}
      </code>
    </pre>
  );
}

function CodeBlock({ code, lang }: { code: string; lang?: string }) {
  const html = useColorized(code, monacoId(lang));
  return <pre>{html ? <code dangerouslySetInnerHTML={{ __html: html }} /> : <code>{code}</code>}</pre>;
}

function InlineCode({ code, lang }: { code: string; lang?: string }) {
  const html = useColorized(code, CODE_LIKE.test(code) ? monacoId(lang) : undefined);
  // One line of code: drop the line break colorize ends with.
  return html ? <code dangerouslySetInnerHTML={{ __html: html.replace(/(<br\s*\/?>)+$/, "") }} /> : <code>{code}</code>;
}

// Inline code is coloured only when it looks like code (a call, a literal,
// an operator), so phrases like `new note: empty title` stay plain.
const CODE_LIKE = /[()[\]{}"'`=<>+*/!&|]|\d/;

/** Code coloured by Monaco's tokenizer with the editor's theme; "" until ready or when there's no language. */
function useColorized(code: string, id: string | undefined): string {
  const [result, setResult] = useState({ code: "", id: "", html: "" });
  useEffect(() => {
    if (!id) return;
    let live = true;
    // colorize escapes the source; the result is only spans with token classes.
    monaco.editor.colorize(code, id, { tabSize: 4 }).then((html) => live && setResult({ code, id, html }));
    return () => {
      live = false;
    };
  }, [code, id]);
  return id && result.code === code && result.id === id ? result.html : "";
}

const monacoId = (lang?: string) => {
  const id = lang && (LANGS[lang.toLowerCase()] ?? lang.toLowerCase());
  return id && known.has(id) ? id : undefined;
};

// Markdown fence names → Monaco language ids.
const LANGS: Record<string, string> = {
  js: "javascript",
  ts: "typescript",
  py: "python",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  yml: "yaml",
  rs: "rust",
  "c++": "cpp",
  golang: "go",
};
const known = new Set(monaco.languages.getLanguages().map((l) => l.id));

// TeX math ($…$, $$…$$) drawn by KaTeX. Formulas that don't parse, such as
// one the tutor is still writing, show their source in grey: red is for
// check results.
const katex: Pluggable = [rehypeKatex, { errorColor: "var(--muted-foreground)" }];

// The same parser ReactMarkdown runs, to find which dollars it reads as math.
const mathParser = unified().use(remarkParse).use(remarkGfm).use(remarkMath);

/**
 * Escapes the dollars that would wrongly start math, so "costs $5 and $10"
 * stays prose. remark-math pairs any two single dollars, as code spans pair
 * backticks. This follows Pandoc's stricter rule: the TeX doesn't start or end
 * with a space, and the closing $ isn't followed by a digit. A pair with a
 * backtick inside isn't math either: it reached into a code span, as in
 * "$5 and `$x$`". Escaping the opener of each such pair and parsing again
 * lets the parser pair the dollars that remain. Code is left alone: the
 * parser never reads math inside it.
 */
function escapeDollarAmounts(text: string): string {
  let source = text;
  while (source.includes("$")) {
    const strays: number[] = [];
    visit(mathParser.parse(source), "inlineMath", (node) => {
      const start = node.position?.start.offset;
      const end = node.position?.end.offset;
      if (start === undefined || end === undefined || source[start + 1] === "$") return;
      const tex = source.slice(start + 1, end - 1);
      if (/^\s|\s$|`/.test(tex) || /\d/.test(source[end] ?? "")) strays.unshift(start);
    });
    if (strays.length === 0) break;
    // From the end, so earlier offsets still hold.
    for (const at of strays) source = `${source.slice(0, at)}\\${source.slice(at)}`;
  }
  return source;
}

/**
 * A paragraph that is only $$…$$ is display math, as on GitHub. remark-math
 * reads $$…$$ on one line as inline math, and only $$ on lines of their own as
 * a block.
 */
function displayMathParagraphs() {
  return (tree: MdastRoot, file: VFile) => {
    visit(tree, "paragraph", (node) => {
      const [math, ...rest] = node.children;
      if (math?.type !== "inlineMath" || rest.length > 0) return;
      if (String(file.value)[(math.position?.start.offset ?? 0) + 1] !== "$") return;
      // rehype-katex draws code with this class in display mode.
      math.data = { ...math.data, hProperties: { className: ["language-math", "math-display"] } };
    });
  };
}

/**
 * Lays out a task statement. Each "Example N" gets its input and output in
 * a box, as on coding-practice sites. Worked examples under "Background"
 * (in older statements "What you need") fold into a closed <details>, titled
 * by the code's first comment line or "Example". The signature at the top
 * stays visible.
 */
function foldExampleCode() {
  return (tree: Root) => {
    let inBackground = false;
    let afterExample = false;
    tree.children = tree.children.map((node) => {
      if (node.type !== "element") return node;
      if (/^h[1-6]$/.test(node.tagName)) {
        const heading = textOf(node).trim();
        inBackground = /^(background|what you need)\b/i.test(heading);
        afterExample = /^example \d+$/i.test(heading);
        return node;
      }
      if (afterExample && node.tagName === "p") {
        afterExample = false;
        node.properties = { ...node.properties, className: ["example-io"] };
        return node;
      }
      if (!inBackground || node.tagName !== "pre") return node;
      const comment = firstComment(textOf(node));
      // A leading comment becomes the summary, so it isn't repeated in the code.
      if (comment) dropFirstLine(node);
      const details: Element = {
        type: "element",
        tagName: "details",
        properties: { className: ["example"] },
        children: [{ type: "element", tagName: "summary", properties: {}, children: [{ type: "text", value: comment ?? "Example" }] }, node],
      };
      return details;
    });
  };
}

function textOf(node: ElementContent | Element): string {
  if (node.type === "text") return node.value;
  if (node.type === "element") return node.children.map(textOf).join("");
  return "";
}

function firstComment(code: string): string | null {
  const m = code.trimStart().match(/^(?:\/\/|#|--)[ \t]*(.+)/);
  return m ? m[1].trim() : null;
}

/** Removes the first line from <pre><code>, whose content is a single text node. */
function dropFirstLine(pre: Element) {
  const code = pre.children[0];
  const text = code?.type === "element" ? code.children[0] : undefined;
  if (text?.type === "text") text.value = text.value.trimStart().replace(/^[^\n]*\n/, "");
}
