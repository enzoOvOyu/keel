import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "cn"
import { Slot } from "radix-ui"

const badgeVariants = cva(
  "ui:inline-flex ui:w-fit ui:shrink-0 ui:items-center ui:justify-center ui:gap-1 ui:overflow-hidden ui:rounded-full ui:border ui:border-transparent ui:px-2 ui:py-0.5 ui:text-xs ui:font-medium ui:whitespace-nowrap ui:transition-[color,box-shadow] ui:focus-visible:border-ring ui:focus-visible:ring-[3px] ui:focus-visible:ring-ring/50 ui:aria-invalid:border-destructive ui:aria-invalid:ring-destructive/20 ui:dark:aria-invalid:ring-destructive/40 ui:[&>svg]:pointer-events-none ui:[&>svg]:size-3",
  {
    variants: {
      variant: {
        default: "ui:bg-primary ui:text-primary-foreground ui:[a&]:hover:bg-primary/90",
        secondary:
          "ui:bg-secondary ui:text-secondary-foreground ui:[a&]:hover:bg-secondary/90",
        destructive:
          "ui:bg-destructive ui:text-white ui:focus-visible:ring-destructive/20 ui:dark:bg-destructive/60 ui:dark:focus-visible:ring-destructive/40 ui:[a&]:hover:bg-destructive/90",
        outline:
          "ui:border-border ui:text-foreground ui:[a&]:hover:bg-accent ui:[a&]:hover:text-accent-foreground",
        ghost: "ui:[a&]:hover:bg-accent ui:[a&]:hover:text-accent-foreground",
        link: "ui:text-primary ui:underline-offset-4 ui:[a&]:hover:underline",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Badge({
  className,
  variant = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span"

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Badge, badgeVariants }
