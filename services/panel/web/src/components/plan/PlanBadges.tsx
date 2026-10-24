import { EyeOff, Gift, Globe, Lock } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { Plan } from "@/lib/types"

export function PlanBadges({ plan }: { plan: Plan }) {
  return (
    <span className="flex flex-wrap gap-1.5">
      {plan.trial && (
        <Badge variant="info">
          <Gift />
          пробный
        </Badge>
      )}
      {plan.visibility === "private" && (
        <Badge variant="default">
          <Lock />
          приватный
        </Badge>
      )}
      {plan.dedicated_ip && (
        <Badge variant="info">
          <Globe />
          выделенный IP
        </Badge>
      )}
      {!plan.on_sale && (
        <Badge variant="outline">
          <EyeOff />
          снят с продажи
        </Badge>
      )}
    </span>
  )
}
