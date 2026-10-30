import type { Inbound, InboundTLS, Node, XHTTPExtra } from "./types"

export type InboundKind = "reality-tcp" | "reality-xhttp" | "tls-xhttp"

export const kindLabels: Record<InboundKind, string> = {
  "reality-tcp": "REALITY · TCP",
  "reality-xhttp": "XHTTP · REALITY",
  "tls-xhttp": "XHTTP · TLS",
}

export const kindHints: Record<InboundKind, string> = {
  "reality-tcp": "VLESS + TCP + REALITY, flow xtls-rprx-vision. Клиент идёт прямо на ноду",
  "reality-xhttp": "VLESS + XHTTP поверх REALITY. Клиент идёт прямо на ноду, без своего домена",
  "tls-xhttp": "VLESS + XHTTP, TLS держит нода на своём домене. Можно поставить CDN перед нодой",
}

export const xhttpModes = ["auto", "packet-up", "stream-up", "stream-one"]
export const alpnOptions = ["h2", "http/1.1", "h3"]
export const minXrayForXHTTP = "26.10.21"

export function kindOf(ib: Pick<Inbound, "transport" | "security">): InboundKind {
  if (ib.transport === "xhttp") return ib.security === "tls" ? "tls-xhttp" : "reality-xhttp"
  return "reality-tcp"
}

export function agentKnowsXHTTP(n: Pick<Node, "last_sync_at" | "agent_xhttp">): boolean {
  return !n.last_sync_at || n.agent_xhttp
}

export function olderThan(version: string | null | undefined, min: string): boolean {
  if (!version) return false
  const parts = (v: string) => v.split(/[^0-9]+/).filter(Boolean).map(Number)
  const a = parts(version)
  const b = parts(min)
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const x = a[i] ?? 0
    const y = b[i] ?? 0
    if (x !== y) return x < y
  }
  return false
}

export interface ClientEndpoint {
  address: string
  port: number
  sni: string
  host: string
}

const isIP = (s: string) => /^[0-9.]+$/.test(s) || s.includes(":")

export function clientEndpoint(t: InboundTLS): ClientEndpoint {
  const address = t.address || t.domain
  const sni = t.sni || (t.address && !isIP(t.address) ? t.address : t.domain)
  return { address, port: t.port || 443, sni, host: t.host || sni }
}

export type ExtraField =
  | { key: string; label: string; hint?: string; kind: "bool" }
  | { key: string; label: string; hint?: string; kind: "text" | "range" | "number"; placeholder?: string }
  | { key: string; label: string; hint?: string; kind: "select"; options: string[]; placeholder: string }
  | { key: string; label: string; hint?: string; kind: "headers" }

export interface ExtraGroup {
  title: string
  hint?: string
  fields: ExtraField[]
}

const placements = ["path", "query", "cookie", "header"]

export const extraGroups: ExtraGroup[] = [
  {
    title: "Паддинг",
    hint: "мусор в запросах, чтобы размеры не выдавали прокси",
    fields: [
      { key: "xPaddingObfsMode", kind: "bool", label: "Обфускация паддинга", hint: "xPaddingObfsMode: паддинг выглядит как обычные данные" },
      { key: "xPaddingMethod", kind: "select", options: ["repeat-x", "tokenish"], placeholder: "repeat-x", label: "Метод" },
      { key: "xPaddingPlacement", kind: "select", options: ["queryInHeader", "cookie", "header", "query"], placeholder: "queryInHeader", label: "Где передавать" },
      { key: "xPaddingKey", kind: "text", label: "Имя ключа", placeholder: "x_padding", hint: "например _ym_d, чтобы кука была как у Метрики" },
      { key: "xPaddingHeader", kind: "text", label: "Заголовок", placeholder: "X-Padding" },
      { key: "xPaddingBytes", kind: "range", label: "Размер", placeholder: "100-1000" },
    ],
  },
  {
    title: "Сессия и порядок пакетов",
    hint: "где клиент передаёт ID сессии и номер пакета",
    fields: [
      { key: "sessionIDPlacement", kind: "select", options: placements, placeholder: "path", label: "ID сессии" },
      { key: "sessionIDKey", kind: "text", label: "Имя ключа ID сессии", placeholder: "x_session" },
      { key: "sessionIDTable", kind: "text", label: "Алфавит ID сессии", placeholder: "Base62", hint: "Base62, hex, number… или свои символы" },
      { key: "sessionIDLength", kind: "range", label: "Длина ID сессии", placeholder: "16-24" },
      { key: "seqPlacement", kind: "select", options: placements, placeholder: "path", label: "Номер пакета" },
      { key: "seqKey", kind: "text", label: "Имя ключа номера", placeholder: "x_seq" },
    ],
  },
  {
    title: "Отправка от клиента",
    fields: [
      { key: "uplinkHTTPMethod", kind: "text", label: "HTTP-метод", placeholder: "POST", hint: "GET — только когда оба mode packet-up" },
      { key: "uplinkDataPlacement", kind: "select", options: ["auto", "body", "cookie", "header"], placeholder: "auto", label: "Где данные", hint: "cookie и header — только когда оба mode packet-up" },
      { key: "uplinkDataKey", kind: "text", label: "Имя ключа данных", placeholder: "X-Data" },
      { key: "uplinkChunkSize", kind: "range", label: "Размер куска", placeholder: "1024-4096" },
    ],
  },
  {
    title: "Заголовки",
    fields: [
      { key: "noSSEHeader", kind: "bool", label: "Без заголовка SSE", hint: "noSSEHeader: сервер не шлёт Content-Type: text/event-stream" },
      { key: "noGRPCHeader", kind: "bool", label: "Без заголовка gRPC", hint: "noGRPCHeader" },
      { key: "headers", kind: "headers", label: "Свои заголовки клиента", hint: "по одному в строке: Имя: значение; Host нельзя" },
    ],
  },
  {
    title: "Лимиты",
    fields: [
      { key: "scMaxEachPostBytes", kind: "range", label: "Макс. размер POST", placeholder: "1000000" },
      { key: "scMinPostsIntervalMs", kind: "range", label: "Пауза между POST, мс", placeholder: "30" },
      { key: "scMaxBufferedPosts", kind: "number", label: "POST в буфере сервера", placeholder: "30" },
      { key: "scStreamUpServerSecs", kind: "range", label: "Пинг stream-up, с", placeholder: "20-80" },
      { key: "serverMaxHeaderBytes", kind: "number", label: "Макс. размер заголовков сервера", placeholder: "8192" },
    ],
  },
  {
    title: "XMUX (клиент)",
    hint: "как клиент переиспользует соединения",
    fields: [
      { key: "xmux.maxConcurrency", kind: "range", label: "maxConcurrency", placeholder: "16-32", hint: "нельзя вместе с maxConnections" },
      { key: "xmux.maxConnections", kind: "range", label: "maxConnections", placeholder: "3" },
      { key: "xmux.cMaxReuseTimes", kind: "range", label: "cMaxReuseTimes", placeholder: "0" },
      { key: "xmux.hMaxRequestTimes", kind: "range", label: "hMaxRequestTimes", placeholder: "600-900" },
      { key: "xmux.hMaxReusableSecs", kind: "range", label: "hMaxReusableSecs", placeholder: "1800-3000" },
      { key: "xmux.hKeepAlivePeriod", kind: "number", label: "hKeepAlivePeriod", placeholder: "0" },
    ],
  },
]

export type ExtraForm = Record<string, string | boolean>

const allFields = extraGroups.flatMap((g) => g.fields)

export function extraToForm(extra: XHTTPExtra | undefined): ExtraForm {
  const form: ExtraForm = {}
  const src = (extra ?? {}) as Record<string, unknown>
  const xmux = (src.xmux ?? {}) as Record<string, unknown>
  for (const f of allFields) {
    const value = f.key.startsWith("xmux.") ? xmux[f.key.slice(5)] : src[f.key]
    if (f.kind === "bool") form[f.key] = value === true
    else if (f.kind === "headers") {
      form[f.key] = Object.entries((value ?? {}) as Record<string, string>)
        .map(([k, v]) => `${k}: ${v}`)
        .join("\n")
    } else form[f.key] = value === undefined || value === null ? "" : String(value)
  }
  return form
}

export function formToExtra(form: ExtraForm): XHTTPExtra {
  const out: Record<string, unknown> = {}
  const xmux: Record<string, unknown> = {}
  for (const f of allFields) {
    const raw = form[f.key]
    let value: unknown
    if (f.kind === "bool") value = raw === true ? true : undefined
    else if (f.kind === "headers") value = parseHeaders(String(raw ?? ""))
    else {
      const s = String(raw ?? "").trim()
      if (s === "") value = undefined
      else if (f.kind === "number" && /^-?\d+$/.test(s)) value = Number(s)
      else value = s
    }
    if (value === undefined) continue
    if (f.key.startsWith("xmux.")) xmux[f.key.slice(5)] = value
    else out[f.key] = value
  }
  if (Object.keys(xmux).length > 0) out.xmux = xmux
  return out as XHTTPExtra
}

function parseHeaders(text: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const line of text.split("\n")) {
    const i = line.indexOf(":")
    if (i <= 0) continue
    const name = line.slice(0, i).trim()
    if (name) out[name] = line.slice(i + 1).trim()
  }
  return Object.keys(out).length > 0 ? out : undefined
}

export function certLine(n: Pick<Node, "tls_status">, domain: string): { ok: boolean; text: string } {
  const st = n.tls_status?.[domain]
  if (!st) return { ok: false, text: "агент ещё не проверял" }
  if (st.not_after) return { ok: true, text: `выпущен, до ${new Date(st.not_after).toLocaleDateString("ru-RU", { timeZone: "Europe/Moscow" })}` }
  return { ok: false, text: `нет серта: ${st.error ?? "неизвестно"}` }
}
