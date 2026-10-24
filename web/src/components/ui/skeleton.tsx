import { cn } from "cn"

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("ui:animate-pulse ui:rounded-md ui:bg-accent", className)}
      {...props}
    />
  )
}

export { Skeleton }
