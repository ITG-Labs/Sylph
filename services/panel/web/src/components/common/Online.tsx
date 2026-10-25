import { fmtDateTime, fmtRelative } from "@/lib/format"
import type { NodeRef } from "@/lib/types"
import { cn } from "@/lib/utils"

export function OnlineDot({ className }: { className?: string }) {
  return (
    <span className={cn("relative inline-flex size-2 shrink-0", className)} aria-hidden>
      <span className="absolute inset-0 animate-ping rounded-full bg-emerald-400/60 motion-reduce:animate-none" />
      <span className="relative size-2 rounded-full bg-emerald-400" />
    </span>
  )
}

export function OnlineState({ nodes, lastSeen }: { nodes: NodeRef[]; lastSeen: string | null }) {
  if (nodes.length > 0) {
    return (
      <span className="inline-flex max-w-full min-w-0 items-center gap-2" title={`онлайн сейчас: ${nodes.map((n) => n.name).join(", ")}`}>
        <OnlineDot />
        <span className="truncate">{nodes.map((n) => n.name).join(", ")}</span>
      </span>
    )
  }
  if (!lastSeen) return <span className="text-muted-foreground">не заходил</span>
  return (
    <span className="text-muted-foreground" title={fmtDateTime(lastSeen)}>
      был {fmtRelative(lastSeen)}
    </span>
  )
}
