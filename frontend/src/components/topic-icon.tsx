import { cn } from "cn";
import { BookOpen, Braces, ChartColumn, Cpu, Database, Globe, Lock, Network, Regex, SquareTerminal, type LucideIcon } from "lucide-react";
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

type Props = {
  name: string;
  size?: number;
  /** Keep the icon's space when there is none, so a list stays aligned. */
  keepSpace?: boolean;
  className?: string;
};

/** A workspace's icon, or nothing until one is picked. */
export function TopicIcon({ name, size = 24, keepSpace, className }: Props) {
  const cls = cn("flex-none text-foreground", className);
  const brand = brands[name];
  if (brand) {
    return (
      <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={cls} aria-hidden="true">
        <path d={brand.path} />
      </svg>
    );
  }
  const Glyph = glyphs[name];
  if (Glyph) return <Glyph size={size} strokeWidth={1.5} className={cls} aria-hidden="true" />;
  return keepSpace ? <span className="flex-none" style={{ width: size, height: size }} aria-hidden="true" /> : null;
}

// A workspace's picture. The names match workspace.Icons in Go, which the
// tutor picks from with set_icon; keep the two lists in step. Languages and
// tools use their logo (Simple Icons, CC0), drawn in one colour. Other topics
// use a Lucide icon.

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

const glyphs: Record<string, LucideIcon> = {
  terminal: SquareTerminal,
  database: Database,
  network: Globe,
  braces: Braces,
  regex: Regex,
  cpu: Cpu,
  chart: ChartColumn,
  lock: Lock,
  tree: Network,
  book: BookOpen,
};
