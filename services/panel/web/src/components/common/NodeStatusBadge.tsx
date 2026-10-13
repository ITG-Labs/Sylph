import { CircleCheck, CircleDashed, CircleOff, CircleX, TriangleAlert } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { NodeStatus } from "@/lib/types"

const map: Record<NodeStatus, { label: string; variant: "success" | "warning" | "destructive" | "secondary" | "outline"; icon: typeof CircleCheck }> = {
  online: { label: "онлайн", variant: "success", icon: CircleCheck },
  degraded: { label: "с проблемой", variant: "warning", icon: TriangleAlert },
  offline: { label: "лежит", variant: "destructive", icon: CircleX },
  never: { label: "не выходила на связь", variant: "secondary", icon: CircleDashed },
  disabled: { label: "выключена", variant: "outline", icon: CircleOff },
}

export function NodeStatusBadge({ status }: { status: NodeStatus }) {
  const s = map[status] ?? map.never
  return (
    <Badge variant={s.variant}>
      <s.icon />
      {s.label}
    </Badge>
  )
}
