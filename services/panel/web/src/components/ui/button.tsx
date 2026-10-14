import * as React from "react"
import { Slot } from "radix-ui"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm font-medium transition-[background-color,box-shadow,color,opacity,scale,filter] duration-200 ease-glide outline-none select-none focus-visible:ring-[3px] focus-visible:ring-ring active:scale-[0.96] disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-primary bg-linear-to-b from-white/22 to-transparent text-primary-foreground shadow-[inset_0_1px_0_0_oklch(1_0_0/45%),inset_0_0_0_1px_oklch(1_0_0/12%),0_8px_22px_-8px_oklch(0.6_0.2_287/70%)] hover:brightness-110",
        destructive:
          "bg-[oklch(0.58_0.2_22)] bg-linear-to-b from-white/18 to-transparent text-white shadow-[inset_0_1px_0_0_oklch(1_0_0/35%),inset_0_0_0_1px_oklch(1_0_0/10%),0_8px_22px_-8px_oklch(0.55_0.2_22/70%)] hover:brightness-110 focus-visible:ring-destructive/40",
        outline: "glass-pill text-foreground hover:bg-white/16",
        secondary: "glass-pill text-secondary-foreground hover:bg-white/16",
        ghost: "hover:bg-white/9 hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline active:scale-100",
      },
      size: {
        default: "h-9 px-4 py-2 has-[>svg]:px-3.5",
        sm: "h-8 gap-1.5 px-3.5 has-[>svg]:px-3",
        lg: "h-11 px-6 text-[15px] has-[>svg]:px-5",
        icon: "size-9",
        "icon-sm": "size-8",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
)

function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<"button"> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "button"
  return <Comp data-slot="button" className={cn(buttonVariants({ variant, size, className }))} {...props} />
}

export { Button, buttonVariants }
