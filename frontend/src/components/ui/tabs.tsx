import * as React from "react";
import { cn } from "@/lib/utils";
import { Tabs as TabsPrimitive } from "radix-ui";

// Tutortop's tabs: grey labels on a divider, the open one bright and
// underlined. Arrow keys move between them.

function Tabs({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn("flex flex-col", className)} {...props} />;
}

function TabsList({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return <TabsPrimitive.List data-slot="tabs-list" className={cn("flex h-10 flex-none items-stretch gap-[18px] border-b", className)} {...props} />;
}

function TabsTrigger({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "-mb-px flex items-center border-b-2 border-transparent text-sm font-medium whitespace-nowrap text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring focus-visible:outline-solid disabled:text-faint data-active:border-foreground data-active:text-foreground",
        className,
      )}
      {...props}
    />
  );
}

/** A tab's panel. It's a tab stop (Radix), so keyboard users can scroll it; the ring shows where focus is. */
function TabsContent({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn(
        "flex-1 outline-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring focus-visible:outline-solid",
        className,
      )}
      {...props}
    />
  );
}

export { Tabs, TabsList, TabsTrigger, TabsContent };
