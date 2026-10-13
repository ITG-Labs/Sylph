import { Progress } from "@/components/ui/progress"
import { fmtBytes, trafficPercent } from "@/lib/format"
import { cn } from "@/lib/utils"

export function TrafficBar({ used, limit, compact }: { used: number; limit: number | null; compact?: boolean }) {
  const pct = trafficPercent(used, limit)
  const color = pct === null ? "bg-info" : pct >= 100 ? "bg-destructive" : pct >= 85 ? "bg-warning" : "bg-primary"
  return (
    <div className={cn("flex flex-col gap-1.5", compact ? "w-36" : "w-full")}>
      <div className="flex items-baseline justify-between gap-2 text-xs tabular-nums">
        <span className={pct !== null && pct >= 100 ? "text-destructive" : "text-foreground"}>{fmtBytes(used)}</span>
        <span className="text-muted-foreground">{limit === null ? "безлимит" : `из ${fmtBytes(limit)}`}</span>
      </div>
      <Progress value={pct ?? 100} className={compact ? "h-1.5" : "h-2"} indicatorClassName={cn(color, pct === null && "opacity-35")} />
    </div>
  )
}
