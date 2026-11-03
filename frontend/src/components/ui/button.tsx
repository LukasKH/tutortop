import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";
import { Slot } from "radix-ui";

// Tutortop's buttons. outline is the everyday button, default the one ink
// button that moves forward, ghost a quiet text button, destructive the one
// red button in a confirmation.
const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-lg border border-transparent text-sm font-medium whitespace-nowrap transition-colors outline-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-45 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "border-primary bg-primary text-primary-foreground hover:border-white hover:bg-white",
        outline: "border-input bg-card text-foreground hover:border-faint",
        ghost: "text-soft-foreground hover:bg-muted hover:text-foreground aria-[current]:font-semibold aria-[current]:text-foreground",
        destructive: "border-destructive text-destructive hover:bg-destructive hover:text-white",
        link: "text-foreground underline-offset-4 hover:underline",
      },
      size: {
        default: "h-8 px-[13px]",
        sm: "h-7 px-2",
        icon: "size-8 rounded-md",
        "icon-sm": "size-7 rounded-md",
      },
    },
    compoundVariants: [{ variant: "ghost", size: "default", className: "px-2" }],
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  }) {
  const Comp = asChild ? Slot.Root : "button";

  return <Comp data-slot="button" data-variant={variant} data-size={size} className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}

export { Button, buttonVariants };
