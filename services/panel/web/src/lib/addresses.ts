import type { NodeAddress, UserAddress } from "./types"

export const addressReason: Record<NonNullable<UserAddress["reason"]>, string> = {
  banned: "юзер забанен",
  no_period: "нет активной подписки",
  node_not_in_plan: "нода адреса не входит в группу нод тарифа",
  plan_without_dedicated_ip: "текущий тариф без пометки «выделенный IP»",
  traffic_exhausted: "трафик периода исчерпан — юзера нет на нодах",
  address_error: "у ноды ошибка с этим адресом",
  address_missing: "нода больше не сообщает этот адрес",
  address_stale: "нода давно не выходила на связь",
  profile_no_direct: "профиль маршрутизации ноды не выпускает трафик этого тарифа напрямую — адрес не используется",
}

export const addressStatus: Record<NodeAddress["status"], { label: string; variant: "success" | "warning" | "destructive" }> = {
  ok: { label: "работает", variant: "success" },
  error: { label: "ошибка", variant: "destructive" },
  missing: { label: "пропал с ноды", variant: "warning" },
}

export function statusOf(a: NodeAddress) {
  return a.stale ? { label: "нода молчит", variant: "warning" as const } : addressStatus[a.status]
}

export function holderLabel(a: NonNullable<NodeAddress["assignment"]>) {
  if (a.telegram_username) return `@${a.telegram_username}`
  if (a.telegram_name) return a.telegram_name
  if (a.telegram_id) return String(a.telegram_id)
  return `юзер № ${a.user_id}`
}

export function assignable(all: NodeAddress[], taken: UserAddress[] | undefined) {
  const busyNodes = new Set((taken ?? []).map((a) => a.node_id))
  return all.filter((a) => a.assignment === null && a.status !== "missing" && !a.stale && !busyNodes.has(a.node_id))
}

export function deletable(a: NodeAddress) {
  return a.status === "missing" || a.stale
}

export function needsAddress(dedicatedPlan: boolean | undefined, addresses: UserAddress[] | undefined) {
  return !!dedicatedPlan && !(addresses ?? []).some((a) => a.effective)
}
