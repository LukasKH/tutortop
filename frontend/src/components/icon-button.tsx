import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

/** An icon-only button; its label is both the tooltip and the accessible name. */
export function IconButton({ label, className, ...props }: ComponentProps<typeof Button> & { label: string }) {
  return <Button variant="ghost" size="icon-sm" className={cn("text-muted-foreground", className)} title={label} aria-label={label} {...props} />;
}
