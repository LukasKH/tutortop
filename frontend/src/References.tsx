import type { Source } from "./api";
import { Link } from "./Markdown";

const host = (url: string) => {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
};

/**
 * Where a module's content comes from, like the sources a course lists: the
 * official pages the plan and its lessons are based on. Each opens in the
 * browser, with its site beside it so the learner sees it's the real thing.
 */
export function References({ sources, label = "Based on" }: { sources?: Source[] | null; label?: string }) {
  if (!sources?.length) return null;
  return (
    <div className="refs">
      <span className="refs-label">{label}</span>
      <ul>
        {sources.map((s) => (
          <li key={s.url}>
            <Link href={s.url}>{s.title}</Link>
            <span className="refs-host">{host(s.url)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
