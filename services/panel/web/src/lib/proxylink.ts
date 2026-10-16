import type { ProxySpec } from "./routing"

export type ParsedLink = { ok: true; proxy: ProxySpec; name: string } | { ok: false; error: string }

const fail = (error: string): ParsedLink => ({ ok: false, error })

function b64(s: string): string | null {
  try {
    const norm = s.replace(/-/g, "+").replace(/_/g, "/")
    const bin = atob(norm + "=".repeat((4 - (norm.length % 4)) % 4))
    return new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)))
  } catch {
    return null
  }
}

function dec(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

function hostPort(host: string, port: string, fallback?: number): { address: string; port: number } | null {
  const address = host.replace(/^\[|\]$/g, "").toLowerCase()
  const p = port === "" ? fallback : Number(port)
  if (!address || !p || !Number.isInteger(p) || p < 1 || p > 65535) return null
  return { address, port: p }
}

function splitHostPort(s: string): { address: string; port: number } | null {
  const m = /^(\[[^\]]+\]|[^:/?#]+):(\d+)$/.exec(s)
  return m ? hostPort(m[1], m[2]) : null
}

function parseSS(rest: string, name: string): ParsedLink {
  const at = rest.lastIndexOf("@")
  let method: string
  let password: string
  let target: string
  if (at >= 0) {
    const user = rest.slice(0, at)
    const tail = rest.slice(at + 1)
    if (/[?/]/.test(tail.replace(/\/$/, ""))) return fail("плагины shadowsocks (obfs, v2ray-plugin) не поддерживаются")
    target = tail.replace(/\/$/, "")
    const plain = dec(user)
    const decoded = plain.includes(":") ? plain : b64(user)
    if (!decoded || !decoded.includes(":")) return fail("не разобрать метод и пароль shadowsocks")
    method = decoded.slice(0, decoded.indexOf(":"))
    password = decoded.slice(decoded.indexOf(":") + 1)
  } else {
    const decoded = b64(rest)
    const i = decoded?.lastIndexOf("@") ?? -1
    if (!decoded || i < 0 || !decoded.slice(0, i).includes(":")) return fail("не разобрать ссылку shadowsocks")
    const cred = decoded.slice(0, i)
    method = cred.slice(0, cred.indexOf(":"))
    password = cred.slice(cred.indexOf(":") + 1)
    target = decoded.slice(i + 1)
  }
  const hp = splitHostPort(target)
  if (!hp) return fail("в ссылке нет адреса и порта")
  if (!method || !password) return fail("не разобрать метод и пароль shadowsocks")
  return { ok: true, name, proxy: { protocol: "shadowsocks", ...hp, method: method.toLowerCase(), password } }
}

export function parseProxyLink(input: string): ParsedLink {
  const link = input.trim()
  const scheme = /^([a-z0-9]+):\/\//i.exec(link)?.[1]?.toLowerCase()
  if (!scheme) return fail("это не ссылка прокси")
  const hash = link.indexOf("#")
  const name = hash >= 0 ? dec(link.slice(hash + 1)).trim() : ""
  const body = hash >= 0 ? link.slice(0, hash) : link
  if (scheme === "ss") return parseSS(body.slice("ss://".length), name)
  if (!["vless", "trojan", "socks", "socks5", "http", "https"].includes(scheme)) return fail(`${scheme}:// не поддерживается — vless, trojan, ss, socks или http`)
  let u: URL
  try {
    u = new URL(body)
  } catch {
    return fail("ссылка битая")
  }
  const q = u.searchParams
  const fallback = scheme === "http" ? 80 : scheme === "https" ? 443 : undefined
  const hp = hostPort(u.hostname, u.port, fallback)
  if (!hp) return fail("в ссылке нет адреса и порта")
  const user = dec(u.username)
  const pass = dec(u.password)
  const opt = (k: string, v: string | null) => (v ? { [k]: v } : {})
  if (scheme === "vless") {
    const type = (q.get("type") ?? "tcp").toLowerCase()
    if (type !== "tcp" && type !== "raw") return fail(`транспорт ${type} пока не поддерживается — только TCP`)
    if (!user) return fail("в ссылке нет UUID")
    const security = (q.get("security") ?? "").toLowerCase()
    if (security !== "reality" && security !== "tls") return fail("нужен vless с security=reality или tls")
    return {
      ok: true,
      name,
      proxy: {
        protocol: "vless",
        ...hp,
        uuid: user.toLowerCase(),
        security,
        ...opt("sni", q.get("sni")?.toLowerCase() ?? null),
        ...opt("fingerprint", q.get("fp")),
        ...opt("public_key", q.get("pbk")),
        ...opt("short_id", q.get("sid")?.toLowerCase() ?? null),
        ...opt("flow", q.get("flow")),
      },
    }
  }
  if (scheme === "trojan") {
    const type = (q.get("type") ?? "tcp").toLowerCase()
    if (type !== "tcp" && type !== "raw") return fail(`транспорт ${type} пока не поддерживается — только TCP`)
    if (!user) return fail("в ссылке нет пароля")
    return { ok: true, name, proxy: { protocol: "trojan", ...hp, password: user, security: "tls", ...opt("sni", q.get("sni")?.toLowerCase() ?? null), ...opt("fingerprint", q.get("fp")) } }
  }
  let username = user
  let password = pass
  if (username && !password) {
    const decoded = b64(u.username)
    if (decoded?.includes(":")) {
      username = decoded.slice(0, decoded.indexOf(":"))
      password = decoded.slice(decoded.indexOf(":") + 1)
    }
  }
  const creds = username ? { username, password } : {}
  if (scheme === "socks" || scheme === "socks5") return { ok: true, name, proxy: { protocol: "socks", ...hp, ...creds } }
  return { ok: true, name, proxy: { protocol: "http", ...hp, ...creds, ...(scheme === "https" ? { security: "tls" } : {}) } }
}
