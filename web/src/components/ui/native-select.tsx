import type { ComponentProps } from "react"
import { cn } from "cn"

/** Native keyboard and form semantics with the shared Keel control tokens. */
export function NativeSelect({ className, ...props }: ComponentProps<"select">) {
  return <select data-slot="native-select" className={cn("ui:h-9 ui:w-full ui:rounded-md ui:border ui:border-input ui:bg-background ui:px-3 ui:text-sm ui:text-foreground ui:outline-none ui:focus-visible:ring-[3px] ui:focus-visible:ring-ring/50 ui:disabled:opacity-50 ui:appearance-auto", className)} {...props} />
}
export function NativeSelectOption(props: ComponentProps<"option">) {
  return <option {...props} />
}
