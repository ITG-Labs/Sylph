import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"
import { cn } from "@/lib/utils"

function Switch({ className, ...props }: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      className={cn(
        "peer inline-flex h-6 w-11 shrink-0 items-center rounded-full border border-transparent shadow-[inset_0_1px_2px_0_oklch(0_0_0/30%)] transition-colors duration-200 outline-none focus-visible:ring-[3px] focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-success/85 data-[state=unchecked]:bg-white/14",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb className="pointer-events-none block h-5 w-5 rounded-full bg-white shadow-[0_2px_6px_0_oklch(0_0_0/35%),inset_0_-1px_0_0_oklch(0_0_0/8%)] ring-0 transition-transform duration-300 ease-spring data-[state=checked]:translate-x-[1.25rem] data-[state=unchecked]:translate-x-0.5" />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
