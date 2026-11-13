export type ClientAction = "direct" | "proxy" | "node" | "block"
export type ClientName = "happ" | "itgray" | "simplexray"

export interface ClientRule {
  name: string
  action: ClientAction
  node_id?: number
  domain?: string[]
  ip?: string[]
  port?: string
  network?: string
}

export interface ClientIssue {
  client: ClientName
  skip: boolean
  reason: string
}

export interface ClientOverlap {
  rule: number
  earlier: number
  value: string
}

export interface ClientRoutingView {
  rules: ClientRule[]
  revision: number
  updated_at: string | null
  geo: { loaded: boolean; release: string; fetched_at: string | null }
  issues: ClientIssue[][]
  missing: string[][]
  overlaps: ClientOverlap[]
}

export interface GeoTags {
  release: string
  geosite: string[]
  geoip: string[]
}

export const actionLabels: Record<ClientAction, string> = {
  direct: "Напрямую",
  proxy: "Через VPN",
  node: "Через ноду",
  block: "Блок",
}

export const clientLabels: Record<ClientName, string> = {
  happ: "Happ",
  itgray: "ITG Ray",
  simplexray: "SimpleXray",
}

export const reasonLabels: Record<string, string> = {
  port: "порт не поддерживается — правило пропущено",
  network: "TCP/UDP не поддерживается — правило пропущено",
  keyword: "keyword: не поддерживается — правило пропущено",
  regexp: "regexp: не поддерживается — правило пропущено",
  category_format: "имя категории не подходит клиенту — правило пропущено",
  domain_format: "домен из одних цифр не подходит клиенту — правило пропущено",
  node_via_active: "«через ноду» не поддерживается — трафик пойдёт через активный сервер",
}

export function moveRule(rules: ClientRule[], from: number, to: number): ClientRule[] {
  if (from < 0 || from >= rules.length || to < 0 || to >= rules.length) return rules
  const next = [...rules]
  const [r] = next.splice(from, 1)
  next.splice(to, 0, r)
  return next
}

function clean(list?: string[]): string[] {
  return (list ?? []).map((v) => v.trim()).filter(Boolean)
}

export function normalizeRule(r: ClientRule): ClientRule {
  const out: ClientRule = { name: r.name.trim(), action: r.action }
  if (r.action === "node" && r.node_id) out.node_id = r.node_id
  const domain = clean(r.domain)
  const ip = clean(r.ip)
  if (domain.length) out.domain = domain
  if (ip.length) out.ip = ip
  if (r.port?.trim()) out.port = r.port.replace(/\s+/g, "")
  if (r.network) out.network = r.network
  return out
}

export function putBody(rules: ClientRule[], revision: number): { rules: ClientRule[]; revision: number } {
  return { rules: rules.map(normalizeRule), revision }
}

export function ruleKey(r: ClientRule): string {
  return JSON.stringify(normalizeRule(r))
}

export function issuesFor(
  saved: Pick<ClientRoutingView, "rules" | "issues" | "missing">,
  r: ClientRule,
): { issues: ClientIssue[]; missing: string[]; checked: boolean } {
  const key = ruleKey(r)
  const i = saved.rules.findIndex((s) => ruleKey(s) === key)
  if (i < 0) return { issues: [], missing: [], checked: false }
  return { issues: saved.issues[i] ?? [], missing: saved.missing[i] ?? [], checked: true }
}
