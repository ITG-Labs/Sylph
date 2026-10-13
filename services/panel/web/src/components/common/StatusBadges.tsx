import { Ban, CircleCheck, CircleDashed, CircleX, Clock, Undo2, AlertTriangle, XCircle } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { PaymentStatus } from "@/lib/types"

export function UserStatusBadge({ status }: { status: "active" | "banned" }) {
  return status === "banned" ? (
    <Badge variant="destructive">
      <Ban />
      забанен
    </Badge>
  ) : (
    <Badge variant="success">
      <CircleCheck />
      активен
    </Badge>
  )
}

const payment: Record<PaymentStatus, { label: string; variant: "success" | "warning" | "destructive" | "secondary" | "info" | "outline"; icon: typeof Clock }> = {
  pending: { label: "ждёт оплаты", variant: "info", icon: Clock },
  succeeded: { label: "оплачен", variant: "success", icon: CircleCheck },
  canceled: { label: "отменён", variant: "secondary", icon: CircleDashed },
  mismatch: { label: "расхождение", variant: "warning", icon: AlertTriangle },
  rejected: { label: "отклонён", variant: "destructive", icon: XCircle },
  refunded: { label: "возвращён", variant: "outline", icon: Undo2 },
}

export function PaymentStatusBadge({ status }: { status: PaymentStatus }) {
  const s = payment[status] ?? { label: status, variant: "secondary" as const, icon: CircleX }
  return (
    <Badge variant={s.variant}>
      <s.icon />
      {s.label}
    </Badge>
  )
}

export function SourceLabel({ source }: { source: string }) {
  return <span className="text-muted-foreground">{source === "yookassa" ? "ЮKassa" : source === "manual" ? "вручную" : source}</span>
}
