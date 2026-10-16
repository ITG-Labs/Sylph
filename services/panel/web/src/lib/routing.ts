import { ApiError } from "./errors"

export type OutboundKind = "direct" | "block" | "proxy" | "cascade" | "raw"
export type Strategy = "random" | "roundRobin" | "leastPing"

export interface ProxySpec {
  protocol: string
  address: string
  port: number
  uuid?: string
  password?: string
  username?: string
  method?: string
  security?: string
  flow?: string
  sni?: string
  fingerprint?: string
  public_key?: string
  short_id?: string
  has_secret?: boolean
}

export interface Outbound {
  tag: string
  kind: OutboundKind
  direct?: { ip: string }
  proxy?: ProxySpec
  cascade?: { node_id: number }
  raw?: unknown
}

export interface Balancer {
  tag: string
  strategy: Strategy
  outbounds: string[]
  fallback?: string
}

export interface Rule {
  name?: string
  domain?: string[]
  ip?: string[]
  port?: string
  network?: string
  protocol?: string[]
  inbound?: string[]
  plans?: number[]
  to: string
}

export interface RoutingDoc {
  outbounds: Outbound[]
  balancers: Balancer[]
  rules: Rule[]
  default: string
  probe: { url: string; interval: string }
}

export interface ProfileWarning {
  outbound: string
  node_id: number
  problem: string
}

export interface Profile {
  id: number
  name: string
  doc: RoutingDoc
  revision: number
  nodes: number
  node_ids: number[]
  warnings: ProfileWarning[] | null
  created_at: string
  updated_at: string
}

export interface GeoSource {
  id: number
  name: string
  kind: "domain" | "ip"
  url: string
  sha256_url: string
  created_at: string
  updated_at: string
}

export interface GeoStatus {
  sha256?: string
  size?: number
  downloaded_at?: string
  error?: string
}

export const tagPattern = /^[a-z0-9-]{1,32}$/
export const sourcePattern = /^[a-z0-9-]{1,32}$/
export const directIPs = [
  { value: "auto", label: "как пришло" },
  { value: "ipv4", label: "только IPv4" },
  { value: "ipv6", label: "только IPv6" },
  { value: "prefer_ipv4", label: "сначала IPv4" },
  { value: "prefer_ipv6", label: "сначала IPv6" },
]
export const ssMethods = [
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-poly1305",
  "chacha20-ietf-poly1305",
  "xchacha20-poly1305",
  "xchacha20-ietf-poly1305",
]
export const sniffProtocols = [
  { value: "http", label: "HTTP" },
  { value: "tls", label: "TLS" },
  { value: "quic", label: "QUIC" },
  { value: "bittorrent", label: "BitTorrent" },
]
export const strategies: { value: Strategy; label: string; hint: string }[] = [
  { value: "random", label: "Случайно", hint: "каждое соединение — на случайный выход" },
  { value: "roundRobin", label: "По кругу", hint: "выходы по очереди" },
  { value: "leastPing", label: "Самый быстрый", hint: "xray меряет задержку и шлёт в самый быстрый живой" },
]
export const kindLabels: Record<OutboundKind, string> = {
  direct: "Напрямую",
  block: "Блок",
  proxy: "Прокси",
  cascade: "Каскад",
  raw: "Сырой JSON",
}
export const protocolLabels: Record<string, string> = {
  vless: "VLESS",
  trojan: "Trojan",
  shadowsocks: "Shadowsocks",
  socks: "SOCKS5",
  http: "HTTP",
}

export function normalizeDoc(doc: Partial<RoutingDoc> | null | undefined): RoutingDoc {
  return {
    outbounds: doc?.outbounds ?? [],
    balancers: doc?.balancers ?? [],
    rules: doc?.rules ?? [],
    default: doc?.default ?? "",
    probe: doc?.probe ?? { url: "", interval: "" },
  }
}

export function starterDoc(): RoutingDoc {
  return {
    outbounds: [
      { tag: "direct", kind: "direct", direct: { ip: "auto" } },
      { tag: "block", kind: "block" },
    ],
    balancers: [],
    rules: [],
    default: "direct",
    probe: { url: "", interval: "" },
  }
}

export const isPlanRule = (r: Rule) => (r.plans?.length ?? 0) > 0

export function orderRules(rules: Rule[]): Rule[] {
  return [...rules.filter((r) => !isPlanRule(r)), ...rules.filter(isPlanRule)]
}

export function targets(doc: RoutingDoc): { value: string; label: string; balancer: boolean }[] {
  return [
    ...doc.outbounds.map((o) => ({ value: o.tag, label: `${o.tag} · ${kindLabels[o.kind]}`, balancer: false })),
    ...doc.balancers.map((b) => ({ value: b.tag, label: `${b.tag} · балансировщик`, balancer: true })),
  ]
}

export function usages(doc: RoutingDoc, tag: string): string[] {
  const out: string[] = []
  doc.rules.forEach((r, i) => {
    if (r.to === tag) out.push(`правило ${i + 1}${r.name ? ` «${r.name}»` : ""}`)
  })
  for (const b of doc.balancers) {
    if (b.tag !== tag && (b.outbounds.includes(tag) || b.fallback === tag)) out.push(`балансировщик ${b.tag}`)
  }
  if (doc.default === tag) out.push("весь остальной трафик")
  return out
}

export function renameTag(doc: RoutingDoc, from: string, to: string): RoutingDoc {
  if (from === to) return doc
  return {
    ...doc,
    rules: doc.rules.map((r) => (r.to === from ? { ...r, to } : r)),
    balancers: doc.balancers.map((b) => ({
      ...b,
      outbounds: b.outbounds.map((t) => (t === from ? to : t)),
      fallback: b.fallback === from ? to : b.fallback,
    })),
    default: doc.default === from ? to : doc.default,
  }
}

export function freeTag(doc: RoutingDoc, base: string): string {
  const taken = new Set([...doc.outbounds.map((o) => o.tag), ...doc.balancers.map((b) => b.tag)])
  const clean = base.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-+|-+$/g, "").replace(/^sylph/, "x").slice(0, 28) || "out"
  if (!taken.has(clean)) return clean
  for (let i = 2; ; i++) if (!taken.has(`${clean}-${i}`)) return `${clean}-${i}`
}

export function tagProblem(doc: RoutingDoc, tag: string, own?: string): string | null {
  if (!tagPattern.test(tag)) return "1–32 знака: a-z, 0-9 и дефис"
  if (tag.startsWith("sylph")) return "теги на sylph заняты системой"
  if (tag !== own && (doc.outbounds.some((o) => o.tag === tag) || doc.balancers.some((b) => b.tag === tag))) return "такой тег уже есть"
  return null
}

export function outboundSummary(o: Outbound, nodeName: (id: number) => string): string {
  switch (o.kind) {
    case "direct":
      return directIPs.find((x) => x.value === (o.direct?.ip ?? "auto"))?.label ?? ""
    case "block":
      return "соединение сбрасывается"
    case "proxy":
      return o.proxy ? `${protocolLabels[o.proxy.protocol] ?? o.proxy.protocol}${o.proxy.security ? ` + ${o.proxy.security.toUpperCase()}` : ""} · ${o.proxy.address}:${o.proxy.port}` : ""
    case "cascade":
      return o.cascade ? `через ноду «${nodeName(o.cascade.node_id)}»` : ""
    case "raw": {
      const p = (o.raw as { protocol?: string } | undefined)?.protocol
      return p ? `протокол ${p}` : ""
    }
  }
}

export interface Chip {
  label: string
  value: string
}

export function ruleChips(r: Rule, planName: (id: number) => string): Chip[] {
  const chips: Chip[] = []
  const list = (label: string, xs: string[] | undefined) => {
    if (!xs?.length) return
    chips.push({ label, value: xs.length <= 2 ? xs.join(", ") : `${xs.slice(0, 2).join(", ")} и ещё ${xs.length - 2}` })
  }
  list("домены", r.domain)
  list("IP", r.ip)
  if (r.port) chips.push({ label: "порты", value: r.port })
  if (r.network) chips.push({ label: "сеть", value: r.network.toUpperCase().replace(",", " + ") })
  list("протокол", r.protocol?.map((p) => sniffProtocols.find((x) => x.value === p)?.label ?? p))
  list("вход", r.inbound)
  list("тариф", r.plans?.map(planName))
  return chips
}

export interface RuleTemplate {
  id: string
  label: string
  hint: string
  rules: (doc: RoutingDoc) => Rule[]
}

function firstOf(doc: RoutingDoc, kind: OutboundKind): string {
  return doc.outbounds.find((o) => o.kind === kind)?.tag ?? ""
}

export const ruleTemplates: RuleTemplate[] = [
  {
    id: "torrents",
    label: "Блок торрентов",
    hint: "BitTorrent → блок; торренты с VPN — частая причина абуз",
    rules: (doc) => [{ name: "Торренты", protocol: ["bittorrent"], to: firstOf(doc, "block") }],
  },
  {
    id: "ads",
    label: "Блок рекламы",
    hint: "домены рекламы и трекеров из geosite:category-ads-all → блок",
    rules: (doc) => [{ name: "Реклама", domain: ["geosite:category-ads-all"], to: firstOf(doc, "block") }],
  },
  {
    id: "ru-direct",
    label: "РФ-сайты напрямую",
    hint: "geosite:category-ru и geoip:ru → напрямую; два правила, потому что условия одного правила должны совпасть все сразу",
    rules: (doc) => [
      { name: "РФ-сайты по доменам", domain: ["geosite:category-ru"], to: firstOf(doc, "direct") },
      { name: "РФ-сайты по IP", ip: ["geoip:ru"], to: firstOf(doc, "direct") },
    ],
  },
]

export function docProblems(doc: RoutingDoc): string[] {
  const out: string[] = []
  if (doc.outbounds.length === 0) out.push("Нужен хотя бы один выход")
  const all = targets(doc).map((t) => t.value)
  if (!doc.default || !all.includes(doc.default)) out.push("Выбери, куда идёт весь остальной трафик")
  doc.rules.forEach((r, i) => {
    if (!all.includes(r.to)) out.push(`Правило ${i + 1}: выбери, куда отправлять`)
  })
  for (const b of doc.balancers) {
    if (b.outbounds.filter((t) => doc.outbounds.some((o) => o.tag === t)).length < 2) out.push(`Балансировщику ${b.tag} нужно хотя бы два выхода`)
  }
  return out
}

export function restartsXray(a: RoutingDoc, b: RoutingDoc): boolean {
  const fileOf = (d: RoutingDoc) => JSON.stringify({ ...d, rules: d.rules.filter((r) => !isPlanRule(r)), byPlan: d.rules.some(isPlanRule) })
  return fileOf(a) !== fileOf(b)
}

const problems: Record<string, string> = {
  missing: "нода удалена",
  disabled: "нода выключена",
  no_address: "у ноды нет адреса",
  private_address: "у ноды внутренний адрес",
  no_inbound: "у ноды нет входа REALITY",
  no_link: "служебный ключ ещё не выдан — появится после сохранения",
}

export function warningText(w: ProfileWarning, nodeName: (id: number) => string): string {
  return `Выход ${w.outbound} → «${nodeName(w.node_id)}»: ${problems[w.problem] ?? w.problem} — пока трафик туда блокируется`
}

const messages: [RegExp, string][] = [
  [/must be a public hostname or IP/, "нужен публичный домен или IP"],
  [/must be a hostname/, "нужен домен"],
  [/must be 1-65535/, "порт 1–65535"],
  [/must be a UUID/, "нужен UUID"],
  [/must be 32 bytes in unpadded base64url/, "публичный ключ — 43 знака base64url"],
  [/must be hex of even length up to 16/, "shortId — hex чётной длины до 16 знаков"],
  [/is required together with password/, "нужен вместе с паролем"],
  [/is required together with username/, "нужен вместе с логином"],
  [/^is required$/, "обязательно"],
  [/secrets are hidden as \*\*\*/, "секреты скрыты звёздочками — введи их заново или не меняй выход"],
  [/must be the tag of an outbound or a balancer/, "нужен тег выхода или балансировщика"],
  [/rules by plan must come after all other rules/, "правила по тарифу должны идти после всех остальных"],
  [/a rule by plan has no other conditions/, "у правила по тарифу не бывает других условий"],
  [/needs at least one condition/, "нужно хотя бы одно условие"],
  [/needs at least two outbounds/, "нужно хотя бы два выхода"],
  [/tags starting with sylph are reserved/, "теги на sylph заняты системой"],
  [/is used twice/, "тег повторяется"],
  [/must be a public https URL/, "нужна публичная ссылка https"],
  [/must be a duration from 10s to 1h/, "интервал от 10s до 1h"],
  [/the profile was changed by someone else/, "профиль уже поменял кто-то другой — обнови страницу"],
  [/cannot cascade to itself/, "нода не может каскадить сама на себя"],
  [/profile is used by nodes/, "профиль назначен нодам — сначала переключи их"],
  [/routing profiles refer to this (name|source)/, "на источник ссылаются профили маршрутизации — сначала поправь их"],
  [/routing profiles use this source as/, "профили используют источник с другим типом — сначала поправь их"],
  [/is not a geo source of domains/, "нет такого источника доменов — заведи его в «Настройки» → «Geo-базы»"],
  [/is not a geo source of IPs/, "нет такого источника IP — заведи его в «Настройки» → «Geo-базы»"],
  [/plan \d+ not found/, "такого тарифа нет"],
  [/^node not found$/, "такой ноды нет"],
  [/this node uses the profile itself/, "эта нода сама на этом профиле — каскад на себя нельзя"],
]

export function translateRouting(msg: string): string {
  return translate(msg)
}

function translate(msg: string): string {
  for (const [re, ru] of messages) if (re.test(msg)) return ru
  return msg
}

const fieldNames: Record<string, string> = {
  tag: "тег",
  address: "адрес",
  port: "порт",
  uuid: "UUID",
  password: "пароль",
  username: "логин",
  method: "метод",
  security: "защита",
  flow: "flow",
  sni: "SNI",
  fingerprint: "отпечаток",
  public_key: "публичный ключ",
  short_id: "shortId",
  raw: "JSON",
  ip: "IP",
  node_id: "нода",
  domain: "домены",
  network: "сеть",
  protocol: "протокол",
  inbound: "вход",
  plans: "тарифы",
  to: "куда",
  name: "название",
  outbounds: "выходы",
  fallback: "запасной выход",
  strategy: "стратегия",
  url: "ссылка",
  interval: "интервал",
}

export function describeRoutingError(err: unknown, doc?: RoutingDoc): string | null {
  if (!(err instanceof ApiError) || !err.field) return null
  const msg = translate(err.message)
  const m = /^doc\.(outbounds|balancers|rules)\[(\d+)\](?:\.(?:proxy|direct|cascade)?\.?([a-z_]+))?$/.exec(err.field)
  if (m) {
    const i = Number(m[2])
    const part = m[3] ? `, ${fieldNames[m[3]] ?? m[3]}` : ""
    if (m[1] === "outbounds") return `Выход ${doc?.outbounds[i]?.tag ?? i + 1}${part}: ${msg}`
    if (m[1] === "balancers") return `Балансировщик ${doc?.balancers[i]?.tag ?? i + 1}${part}: ${msg}`
    const r = doc?.rules[i]
    return `Правило ${i + 1}${r?.name ? ` «${r.name}»` : ""}${part}: ${msg}`
  }
  if (err.field === "doc.default") return `Весь остальной трафик: ${msg}`
  if (err.field.startsWith("doc.probe")) return `Проверка скорости: ${msg}`
  if (err.field === "doc") return `Профиль: ${msg}`
  if (err.field === "revision") return msg
  if (err.field === "profile_id") return `Профиль: ${msg}`
  return null
}
