import * as React from "react"
import { Tabs as TabsPrimitive } from "radix-ui"
import { motion } from "motion/react"
import { cn } from "@/lib/utils"

const TabsContext = React.createContext<{ value: string | undefined; id: string }>({ value: undefined, id: "" })

function Tabs({ className, value, defaultValue, onValueChange, ...props }: React.ComponentProps<typeof TabsPrimitive.Root>) {
  const id = React.useId()
  const [inner, setInner] = React.useState(defaultValue)
  const current = value ?? inner
  const change = (v: string) => {
    setInner(v)
    onValueChange?.(v)
  }
  return (
    <TabsContext.Provider value={{ value: current, id }}>
      <TabsPrimitive.Root data-slot="tabs" className={cn("flex flex-col gap-3", className)} value={current} onValueChange={change} {...props} />
    </TabsContext.Provider>
  )
}

function TabsList({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        "field inline-flex h-10 w-fit max-w-full items-center justify-center overflow-x-auto rounded-full border border-white/6 p-1 text-muted-foreground",
        className,
      )}
      {...props}
    />
  )
}

function TabsTrigger({ className, value, children, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  const ctx = React.useContext(TabsContext)
  const active = ctx.value === value
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      value={value}
      className={cn(
        "relative inline-flex h-full flex-1 items-center justify-center gap-1.5 rounded-full px-3.5 py-1 text-sm font-medium whitespace-nowrap transition-colors duration-200 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 data-[state=active]:text-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      {...props}
    >
      {active && (
        <motion.span
          layoutId={`${ctx.id}-tab`}
          className="glass-pill absolute inset-0 rounded-full"
          transition={{ type: "spring", bounce: 0.22, duration: 0.5 }}
        />
      )}
      <span className="relative inline-flex items-center gap-1.5">{children}</span>
    </TabsPrimitive.Trigger>
  )
}

function TabsContent({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content data-slot="tabs-content" className={cn("flex-1 outline-none", className)} {...props} />
}

export { Tabs, TabsList, TabsTrigger, TabsContent }
