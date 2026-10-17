import type { ServiceRole } from "./types"

const titles: Record<ServiceRole, string> = { bot: "Бот", subs: "subs", cron: "Крон", panel: "Панель", monitor: "Монитор" }

export function serviceTitle(role: ServiceRole, cn: string): string {
  const name = titles[role] ?? role
  return cn === role ? name : `${name} (${cn})`
}

export type CertTone = "none" | "ok" | "soon" | "urgent" | "expired"

export function certState(notAfter: string | null, now: Date = new Date()): { tone: CertTone; days: number | null } {
  if (!notAfter) return { tone: "none", days: null }
  const ms = new Date(notAfter).getTime() - now.getTime()
  const days = Math.floor(ms / 86400_000)
  if (ms <= 0) return { tone: "expired", days }
  if (days <= 7) return { tone: "urgent", days }
  if (days <= 30) return { tone: "soon", days }
  return { tone: "ok", days }
}
