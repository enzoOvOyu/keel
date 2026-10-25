import type { ComponentProps } from "react"
import { Checkbox as Primitive } from "radix-ui"
import { CheckIcon } from "lucide-react"
import { cn } from "cn"

export function Checkbox({ className, ...props }: ComponentProps<typeof Primitive.Root>) {
  return <Primitive.Root data-slot="checkbox" className={cn("ui:size-4 ui:shrink-0 ui:rounded-sm ui:border ui:border-input ui:bg-background ui:text-primary-foreground ui:outline-none ui:focus-visible:ring-[3px] ui:focus-visible:ring-ring/50 ui:data-[state=checked]:bg-primary ui:disabled:opacity-50 ui:disabled:cursor-not-allowed", className)} {...props}>
    <Primitive.Indicator className="ui:flex ui:items-center ui:justify-center"><CheckIcon className="ui:size-3.5" /></Primitive.Indicator>
  </Primitive.Root>
}

/** Native form/change-event semantics for existing forms and row selection. */
export function NativeCheckbox({ className, ...props }: Omit<ComponentProps<"input">, "type">) {
  return <input {...props} type="checkbox" data-slot="native-checkbox" className={cn("ui:size-4 ui:shrink-0 ui:accent-primary ui:appearance-auto ui:cursor-pointer ui:focus-visible:outline-2 ui:focus-visible:outline-ring ui:disabled:cursor-not-allowed ui:disabled:opacity-50", className)} />
}
