import type { Source } from "@/lib/api";
import { Link } from "@/components/markdown";

/**
 * Where a module's content comes from, like the sources a course lists: the
 * official pages the plan and its lessons are based on. Each opens in the
 * browser, with its site beside it so the learner sees it's the real thing.
 */
export function References({ sources, label = "Based on" }: { sources?: Source[] | null; label?: string }) {
  if (!sources?.length) return null;
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <ul className="flex flex-col gap-[3px]">
        {sources.map((s) => (
          <li
            key={s.url}
            className="flex min-w-0 items-baseline gap-2 text-sm [&_a]:text-soft-foreground [&_a]:underline [&_a]:decoration-input [&_a]:underline-offset-3 [&_a:hover]:text-foreground [&_a:hover]:decoration-soft-foreground"
          >
            <Link href={s.url}>{s.title}</Link>
            <span className="font-mono text-xs leading-[normal] whitespace-nowrap text-faint">{host(s.url)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

const host = (url: string) => {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
};
