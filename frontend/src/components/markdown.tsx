import { useEffect, useMemo, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Element, ElementContent, Root } from "hast";
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
  return (
    <div className={className}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={foldExamples ? [foldExampleCode] : []} components={components}>
        {text}
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
  const [html, setHtml] = useState("");
  useEffect(() => {
    setHtml("");
    if (!id) return;
    let live = true;
    // colorize escapes the source; the result is only spans with token classes.
    monaco.editor.colorize(code, id, { tabSize: 4 }).then((h) => live && setHtml(h));
    return () => {
      live = false;
    };
  }, [code, id]);
  return html;
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
