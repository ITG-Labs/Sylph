import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

const tones = {
  violet: { glow: "from-violet-500/28", chip: "text-violet-300" },
  green: { glow: "from-emerald-400/22", chip: "text-emerald-300" },
  teal: { glow: "from-cyan-400/24", chip: "text-cyan-300" },
  blue: { glow: "from-sky-400/22", chip: "text-sky-300" },
}

export function StatTile({
  label,
  value,
  hint,
  icon,
  tone = "violet",
  className,
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
  icon?: ReactNode
  tone?: keyof typeof tones
  className?: string
}) {
  const t = tones[tone]
  return (
    <div className={cn("glass-thin relative isolate flex h-full flex-col gap-3 overflow-hidden rounded-2xl p-5", className)}>
      <span aria-hidden className={cn("pointer-events-none absolute -top-16 -right-16 -z-10 size-48 rounded-full bg-radial to-transparent to-70%", t.glow)} />
      <div className="flex items-center justify-between gap-2 text-sm text-muted-foreground">
        <span>{label}</span>
        {icon && (
          <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-full bg-white/8 shadow-[inset_0_1px_0_0_oklch(1_0_0/14%)] [&_svg]:size-4", t.chip)}>{icon}</span>
        )}
      </div>
      <div className="text-3xl font-semibold tracking-[-0.03em] tabular-nums">{value}</div>
      {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
    </div>
  )
}
