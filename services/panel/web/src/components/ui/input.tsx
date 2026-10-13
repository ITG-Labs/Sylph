import * as React from "react"
import { cn } from "@/lib/utils"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "field h-9 w-full min-w-0 rounded-xl border border-white/8 px-3 py-1 text-base transition-[color,box-shadow,border-color] outline-none placeholder:text-muted-foreground/80 selection:bg-primary selection:text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
        "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40",
        "aria-invalid:border-destructive aria-invalid:ring-destructive/30",
        className,
      )}
      {...props}
    />
  )
}

export { Input }
