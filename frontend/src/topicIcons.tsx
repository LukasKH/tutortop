import type { ReactNode } from "react";
import {
  siC,
  siClojure,
  siCplusplus,
  siDocker,
  siDotnet,
  siElixir,
  siGit,
  siGleam,
  siGnubash,
  siGo,
  siHaskell,
  siHtmx,
  siJavascript,
  siJulia,
  siKotlin,
  siKubernetes,
  siLinux,
  siLua,
  siNodedotjs,
  siNumpy,
  siOcaml,
  siOpenjdk,
  siPandas,
  siPhp,
  siPostgresql,
  siPython,
  siR,
  siReact,
  siRedis,
  siRuby,
  siRust,
  siScala,
  siSqlite,
  siSwift,
  siTypescript,
  siWebassembly,
  siZig,
  type SimpleIcon,
} from "simple-icons";

// A workspace's picture. The names match workspace.Icons in Go, which the
// coach picks from with set_icon; keep the two lists in step. Languages and
// tools use their logo (Simple Icons, CC0), drawn in one colour. Other topics
// use a glyph on the same 24px grid.

const brands: Record<string, SimpleIcon> = {
  go: siGo,
  python: siPython,
  rust: siRust,
  typescript: siTypescript,
  javascript: siJavascript,
  c: siC,
  cplusplus: siCplusplus,
  java: siOpenjdk,
  kotlin: siKotlin,
  swift: siSwift,
  csharp: siDotnet,
  haskell: siHaskell,
  elixir: siElixir,
  ruby: siRuby,
  php: siPhp,
  lua: siLua,
  zig: siZig,
  ocaml: siOcaml,
  r: siR,
  julia: siJulia,
  scala: siScala,
  clojure: siClojure,
  gleam: siGleam,
  bash: siGnubash,
  sqlite: siSqlite,
  postgresql: siPostgresql,
  redis: siRedis,
  docker: siDocker,
  kubernetes: siKubernetes,
  git: siGit,
  linux: siLinux,
  htmx: siHtmx,
  react: siReact,
  nodejs: siNodedotjs,
  wasm: siWebassembly,
  numpy: siNumpy,
  pandas: siPandas,
};

const glyphs: Record<string, ReactNode> = {
  terminal: (
    <>
      <rect x="3" y="4.5" width="18" height="15" rx="2" />
      <path d="M7 9.5l3 2.5-3 2.5M12.5 15H17" />
    </>
  ),
  database: (
    <>
      <ellipse cx="12" cy="6" rx="7" ry="2.5" />
      <path d="M5 6v12c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5V6M5 12c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5" />
    </>
  ),
  network: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17M12 3.5c2.4 2.5 3.5 5.3 3.5 8.5s-1.1 6-3.5 8.5c-2.4-2.5-3.5-5.3-3.5-8.5s1.1-6 3.5-8.5z" />
    </>
  ),
  braces: (
    <path d="M8.5 4C6.5 4 6 5 6 6.5v3c0 1.5-.8 2.5-2 2.5 1.2 0 2 1 2 2.5v3C6 19 6.5 20 8.5 20M15.5 4c2 0 2.5 1 2.5 2.5v3c0 1.5.8 2.5 2 2.5-1.2 0-2 1-2 2.5v3c0 1.5-.5 2.5-2.5 2.5" />
  ),
  regex: (
    <>
      <path d="M16 4v8M12.5 6l7 4M19.5 6l-7 4" />
      <rect x="4.5" y="15.5" width="4" height="4" rx="0.5" fill="currentColor" stroke="none" />
    </>
  ),
  cpu: (
    <>
      <rect x="6.5" y="6.5" width="11" height="11" rx="1.5" />
      <rect x="10" y="10" width="4" height="4" />
      <path d="M10 3v3.5M14 3v3.5M10 17.5V21M14 17.5V21M3 10h3.5M3 14h3.5M17.5 10H21M17.5 14H21" />
    </>
  ),
  chart: <path d="M4 4v16h16M8.5 16v-4M12.5 16V8M16.5 16v-6" />,
  lock: (
    <>
      <rect x="5" y="10.5" width="14" height="9.5" rx="2" />
      <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5" />
    </>
  ),
  tree: (
    <>
      <circle cx="12" cy="5" r="2" />
      <circle cx="6" cy="18.5" r="2" />
      <circle cx="18" cy="18.5" r="2" />
      <path d="M11.2 6.9l-4.4 9.7M12.8 6.9l4.4 9.7" />
    </>
  ),
  book: (
    <path d="M4 5.5C4 4.7 4.7 4 5.5 4H11v16H5.5c-.8 0-1.5-.7-1.5-1.5zM20 5.5c0-.8-.7-1.5-1.5-1.5H13v16h5.5c.8 0 1.5-.7 1.5-1.5z" />
  ),
};

type Props = {
  name: string;
  size?: number;
  /** Keep the icon's space when there is none, so a list stays aligned. */
  keepSpace?: boolean;
  className?: string;
};

/** A workspace's icon, or nothing until one is picked. */
export function TopicIcon({ name, size = 24, keepSpace, className }: Props) {
  const cls = `topic-icon ${className ?? ""}`;
  const brand = brands[name];
  if (brand) {
    return (
      <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={cls} aria-hidden="true">
        <path d={brand.path} />
      </svg>
    );
  }
  const glyph = glyphs[name];
  if (glyph) {
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        className={cls}
        aria-hidden="true"
      >
        {glyph}
      </svg>
    );
  }
  return keepSpace ? <span className="topic-icon" style={{ width: size, height: size }} aria-hidden="true" /> : null;
}
