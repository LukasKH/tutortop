import type { ComponentProps, ReactNode } from "react";
import { cn } from "cn";

/** A page in the workspace's centre column: a header over content that fills the rest. */
export function Page({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex min-h-0 flex-1 flex-col", className)} {...props} />;
}

/**
 * The page's top bar: where it is ("Module / Lesson: Title"), then actions on
 * the right.
 */
export function PageHeader({
  module,
  label,
  title,
  tooltip = title,
  children,
}: {
  module?: string;
  label?: string;
  title: string;
  /** Shown on hover; the title by default. */
  tooltip?: string;
  children?: ReactNode;
}) {
  return (
    <header className="flex h-[50px] flex-none items-center gap-2.5 border-b px-4">
      <div className="min-w-0 flex-1 truncate text-sm" title={tooltip}>
        {module && <span className="text-muted-foreground">{module} / </span>}
        {label && <span className="text-muted-foreground">{label} </span>}
        <b className="font-semibold">{title}</b>
      </div>
      {children}
    </header>
  );
}

/** A page's headline, with a line of text under it. */
export function PageIntro({ title, children }: { title: ReactNode; children?: ReactNode }) {
  return (
    <header>
      <h1 className="mb-2.5 text-[30px] leading-[1.15] font-bold">{title}</h1>
      {children && <p className="text-lg leading-[1.6] text-soft-foreground">{children}</p>}
    </header>
  );
}

/** A scrolling page of text, kept to a readable line length. */
export function ReadingPage({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex-1 overflow-auto px-12 py-10 *:max-w-[64ch]", className)} {...props} />;
}
