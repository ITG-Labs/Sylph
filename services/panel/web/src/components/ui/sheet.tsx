import * as React from "react"
import { Dialog as SheetPrimitive } from "radix-ui"
import { cn } from "@/lib/utils"

function Sheet(props: React.ComponentProps<typeof SheetPrimitive.Root>) {
  return <SheetPrimitive.Root data-slot="sheet" {...props} />
}

function SheetTrigger(props: React.ComponentProps<typeof SheetPrimitive.Trigger>) {
  return <SheetPrimitive.Trigger data-slot="sheet-trigger" {...props} />
}

function SheetContent({ className, children, ...props }: React.ComponentProps<typeof SheetPrimitive.Content>) {
  return (
    <SheetPrimitive.Portal>
      <SheetPrimitive.Overlay className="fixed inset-0 z-50 bg-(--scrim) data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:duration-300 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:duration-200" />
      <SheetPrimitive.Content
        data-slot="sheet-content"
        className={cn(
          "glass-strong fixed top-2 bottom-2 left-2 z-50 flex w-72 max-w-[calc(100vw-3rem)] flex-col rounded-[1.75rem] data-[state=open]:animate-in data-[state=open]:slide-in-from-left-[110%] data-[state=open]:duration-500 data-[state=open]:ease-spring data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left-[110%] data-[state=closed]:duration-250 data-[state=closed]:ease-glide",
          className,
        )}
        {...props}
      >
        {children}
      </SheetPrimitive.Content>
    </SheetPrimitive.Portal>
  )
}

function SheetTitle({ className, ...props }: React.ComponentProps<typeof SheetPrimitive.Title>) {
  return <SheetPrimitive.Title data-slot="sheet-title" className={cn("sr-only", className)} {...props} />
}

function SheetDescription({ className, ...props }: React.ComponentProps<typeof SheetPrimitive.Description>) {
  return <SheetPrimitive.Description data-slot="sheet-description" className={cn("sr-only", className)} {...props} />
}

export { Sheet, SheetTrigger, SheetContent, SheetTitle, SheetDescription }
