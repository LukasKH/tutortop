import type { ComponentProps, ReactNode } from "react";
import { cn } from "cn";

/** The heading over a curriculum: "Curriculum", "Course plan". */
export function CurriculumTitle({ className, ...props }: ComponentProps<"h1">) {
  return <h1 className={cn("text-[24px] leading-[1.2] font-bold", className)} {...props} />;
}

/** The modules in order. */
export function ModuleList({ className, ...props }: ComponentProps<"ol">) {
  return <ol className={cn("mt-7 flex flex-col gap-[22px]", className)} {...props} />;
}

/** One module: its heading over its objectives. */
export function Module({ heading, children }: { heading: ReactNode; children: ReactNode }) {
  return (
    <li>
      <ModuleHeading>{heading}</ModuleHeading>
      <ul>{children}</ul>
    </li>
  );
}

/** A module's title, with any notes beside it. */
export function ModuleHeading({ children }: { children: ReactNode }) {
  return <h2 className="flex items-baseline gap-2.5 border-b pb-2 text-lg font-bold">{children}</h2>;
}

/** Grey text beside a module's title: "2/5", "moved". */
export function ModuleNote({ className, ...props }: ComponentProps<"span">) {
  return <span className={cn("text-sm font-normal text-muted-foreground", className)} {...props} />;
}

/** An objective: a ✓ once passed, then its short name with the full goal the tests prove under it. */
export function Objective({ done, children }: { done: boolean; children: ReactNode }) {
  return (
    <li className="flex items-baseline gap-2 border-b py-2.5 text-base text-foreground">
      <span className="w-3 flex-none text-[11px] text-pass">{done ? "✓" : ""}</span>
      <span className="flex min-w-0 flex-col gap-[3px] leading-[1.45]">{children}</span>
    </li>
  );
}

/** An objective's name. */
export function ObjectiveName({ children }: { children: ReactNode }) {
  return <span className="font-medium text-foreground">{children}</span>;
}

/** Grey text in an objective: its full goal, its language. */
export function ObjectiveNote({ className, ...props }: ComponentProps<"span">) {
  return <span className={cn("text-sm/[1.45] text-muted-foreground", className)} {...props} />;
}
