import { cn } from "@/lib/utils";
import { Loader2Icon } from "lucide-react";

// Not a live region: it sits inside rows that already announce what's happening.
function Spinner({ className, ...props }: React.ComponentProps<"svg">) {
  return <Loader2Icon data-slot="spinner" role="img" aria-label="Working" className={cn("size-4 animate-spin", className)} {...props} />;
}

export { Spinner };
