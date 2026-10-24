import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "cn"

const alertVariants = cva(
  "ui:relative ui:grid ui:w-full ui:grid-cols-[0_1fr] ui:items-start ui:gap-y-0.5 ui:rounded-lg ui:border ui:px-4 ui:py-3 ui:text-sm ui:has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] ui:has-[>svg]:gap-x-3 ui:[&>svg]:size-4 ui:[&>svg]:translate-y-0.5 ui:[&>svg]:text-current",
  {
    variants: {
      variant: {
        default: "ui:bg-card ui:text-card-foreground",
        destructive:
          "ui:bg-card ui:text-destructive ui:*:data-[slot=alert-description]:text-destructive/90 ui:[&>svg]:text-current",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  )
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn(
        "ui:col-start-2 ui:line-clamp-1 ui:min-h-4 ui:font-medium ui:tracking-tight",
        className
      )}
      {...props}
    />
  )
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn(
        "ui:col-start-2 ui:grid ui:justify-items-start ui:gap-1 ui:text-sm ui:text-muted-foreground ui:[&_p]:leading-relaxed",
        className
      )}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription }
