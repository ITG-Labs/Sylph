import { useEffect, useState } from "react"
import { ClipboardPaste } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { fingerprints } from "@/components/node/NodeDialogs"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { parseProxyLink } from "@/lib/proxylink"
import { directIPs, freeTag, kindLabels, protocolLabels, ssMethods, tagProblem, type Outbound, type OutboundKind, type ProxySpec, type RoutingDoc } from "@/lib/routing"
import type { Node } from "@/lib/types"
import { Field } from "./parts"

const kinds: { value: OutboundKind; hint: string }[] = [
  { value: "direct", hint: "в интернет прямо с ноды" },
  { value: "block", hint: "соединение сбрасывается" },
  { value: "proxy", hint: "через чужой прокси: VLESS, Trojan, Shadowsocks, SOCKS5, HTTP" },
  { value: "cascade", hint: "через другую нашу ноду — ядро само выдаст ей служебный ключ" },
  { value: "raw", hint: "outbound xray как есть — для WARP и прочего, чего нет в формах" },
]

const blankProxy = (): ProxySpec => ({ protocol: "vless", address: "", port: 443, security: "reality", flow: "xtls-rprx-vision", fingerprint: "chrome" })

function cleanProxy(p: ProxySpec): ProxySpec {
  const out: ProxySpec = { protocol: p.protocol, address: p.address.trim().toLowerCase(), port: Number(p.port) }
  const keep = (k: keyof ProxySpec, v: string | undefined) => {
    const t = v?.trim()
    if (t) (out as unknown as Record<string, unknown>)[k] = t
  }
  switch (p.protocol) {
    case "vless":
      keep("uuid", p.uuid?.toLowerCase())
      keep("security", p.security)
      keep("flow", p.flow)
      keep("sni", p.sni?.toLowerCase())
      keep("fingerprint", p.fingerprint)
      if (p.security === "reality") {
        keep("public_key", p.public_key)
        keep("short_id", p.short_id?.toLowerCase())
      }
      break
    case "trojan":
      keep("password", p.password)
      out.security = "tls"
      keep("sni", p.sni?.toLowerCase())
      keep("fingerprint", p.fingerprint)
      break
    case "shadowsocks":
      keep("method", p.method)
      keep("password", p.password)
      break
    case "socks":
    case "http":
      keep("username", p.username)
      keep("password", p.password)
      if (p.protocol === "http" && p.security === "tls") {
        out.security = "tls"
        keep("sni", p.sni?.toLowerCase())
      }
      break
  }
  return out
}

function proxyMissing(p: ProxySpec, hasSecret: boolean): string | null {
  if (!p.address.trim()) return "нужен адрес"
  const port = Number(p.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return "порт 1–65535"
  const secret = (v?: string) => hasSecret || !!v?.trim()
  switch (p.protocol) {
    case "vless":
      if (!secret(p.uuid)) return "нужен UUID"
      if (p.security === "reality" && (!p.public_key?.trim() || !p.sni?.trim())) return "для REALITY нужны SNI и публичный ключ"
      return null
    case "trojan":
      return secret(p.password) ? null : "нужен пароль"
    case "shadowsocks":
      return !p.method ? "выбери метод" : secret(p.password) ? null : "нужен пароль"
    default:
      if (!!p.username?.trim() !== secret(p.password)) return "логин и пароль — оба или никакого"
      return null
  }
}

export function OutboundDialog({
  open,
  onOpenChange,
  doc,
  outbound,
  nodes,
  profileNodes,
  onSave,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  doc: RoutingDoc
  outbound?: Outbound
  nodes: Node[]
  profileNodes: number[]
  onSave: (o: Outbound) => void
}) {
  const [tag, setTag] = useState("")
  const [kind, setKind] = useState<OutboundKind>("direct")
  const [ip, setIp] = useState("auto")
  const [proxy, setProxy] = useState<ProxySpec>(blankProxy())
  const [nodeId, setNodeId] = useState("")
  const [raw, setRaw] = useState("")
  const [link, setLink] = useState("")
  const [linkError, setLinkError] = useState("")

  useEffect(() => {
    if (!open) return
    setTag(outbound?.tag ?? freeTag(doc, "out"))
    setKind(outbound?.kind ?? "proxy")
    setIp(outbound?.direct?.ip ?? "auto")
    setProxy(outbound?.proxy ? { ...blankProxy(), flow: "", fingerprint: "", ...outbound.proxy } : blankProxy())
    setNodeId(outbound?.cascade ? String(outbound.cascade.node_id) : "")
    setRaw(outbound?.raw ? JSON.stringify(outbound.raw, null, 2) : "")
    setLink("")
    setLinkError("")
  }, [open, outbound, doc])

  const kept = !!outbound?.proxy?.has_secret && outbound.kind === "proxy"
  const bare = (p: ProxySpec) => JSON.stringify({ ...cleanProxy(p), uuid: undefined, password: undefined })
  const moved = kept && kind === "proxy" && (tag.trim() !== outbound!.tag || bare(proxy) !== bare({ ...blankProxy(), flow: "", fingerprint: "", ...outbound!.proxy! }))
  const rawRenamed = !!outbound && outbound.kind === "raw" && kind === "raw" && tag.trim() !== outbound.tag && raw.includes('"***"')
  const hasSecret = kept && !moved
  const set = (patch: Partial<ProxySpec>) => setProxy((p) => ({ ...p, ...patch }))
  const tagErr = tagProblem(doc, tag.trim(), outbound?.tag)
  let rawObj: unknown = null
  let rawErr: string | null = null
  if (kind === "raw") {
    try {
      rawObj = JSON.parse(raw)
      if (!rawObj || typeof rawObj !== "object" || Array.isArray(rawObj)) rawErr = "нужен JSON-объект"
      else if (typeof (rawObj as { protocol?: unknown }).protocol !== "string") rawErr = "в объекте нужен protocol"
    } catch {
      rawErr = raw.trim() ? "JSON не разбирается" : "вставь outbound xray"
    }
  }
  const missing =
    kind === "proxy"
      ? moved && !proxy.uuid?.trim() && !proxy.password?.trim()
        ? `параметры или тег выхода поменялись — введи ${proxy.protocol === "vless" ? "UUID" : "пароль"} заново, старый к другим параметрам не подставляется`
        : proxyMissing(proxy, hasSecret)
      : kind === "cascade" && !nodeId
        ? "выбери ноду"
        : kind === "raw"
          ? (rawErr ?? (rawRenamed ? "у переименованного выхода замени *** настоящими значениями — старые секреты подставляются только к тому же тегу" : null))
          : null
  const candidates = nodes.filter((n) => !profileNodes.includes(n.id))

  const pasteLink = () => {
    const r = parseProxyLink(link)
    if (!r.ok) {
      setLinkError(r.error)
      return
    }
    setLinkError("")
    setKind("proxy")
    setProxy({ ...blankProxy(), flow: "", fingerprint: "", ...r.proxy })
    if (!outbound && r.name) setTag(freeTag(doc, r.name))
  }

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={outbound ? `Выход ${outbound.tag}` : "Новый выход"}
      description="куда уходит трафик, который на него направили правила"
      consequencesBelow
      consequences={missing ? <p>{missing}</p> : undefined}
      confirmLabel={outbound ? "Готово" : "Добавить"}
      canConfirm={!tagErr && !missing}
      onConfirm={async () => {
        const t = tag.trim()
        const o: Outbound = { tag: t, kind }
        if (kind === "direct") o.direct = { ip }
        if (kind === "proxy") {
          o.proxy = cleanProxy(proxy)
          if (hasSecret && !o.proxy.uuid && !o.proxy.password) o.proxy.has_secret = true
        }
        if (kind === "cascade") o.cascade = { node_id: Number(nodeId) }
        if (kind === "raw") o.raw = rawObj
        onSave(o)
      }}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field id="ob-tag" label="Тег" error={tag.trim() ? tagErr : null} hint="на него ссылаются правила">
          <Input id="ob-tag" value={tag} onChange={(e) => setTag(e.target.value)} />
        </Field>
        <Field label="Вид" hint={kinds.find((k) => k.value === kind)?.hint}>
          <Select value={kind} onValueChange={(v) => setKind(v as OutboundKind)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {kinds.map((k) => (
                <SelectItem key={k.value} value={k.value}>
                  {kindLabels[k.value]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>

      {kind === "direct" && (
        <Field label="Адреса назначения" hint="какие IP брать, когда сайт отдаёт и IPv4, и IPv6">
          <Select value={ip} onValueChange={setIp}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {directIPs.map((x) => (
                <SelectItem key={x.value} value={x.value}>
                  {x.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      )}

      {kind === "proxy" && (
        <>
          <div className="well flex flex-col gap-2 rounded-xl p-3">
            <div className="flex gap-2">
              <Input aria-label="Ссылка прокси" value={link} onChange={(e) => setLink(e.target.value)} placeholder="vless://… trojan://… ss://… socks://… http://…" className="font-mono text-xs" />
              <Button type="button" variant="secondary" onClick={pasteLink} disabled={!link.trim()}>
                <ClipboardPaste />
                Разобрать
              </Button>
            </div>
            <p className={linkError ? "text-xs text-[oklch(0.82_0.12_22)]" : "text-xs text-muted-foreground"}>{linkError || "вставь ссылку — форма заполнится сама"}</p>
          </div>
          <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
            <Field label="Протокол">
              <Select value={proxy.protocol} onValueChange={(v) => set({ protocol: v, security: v === "vless" ? "reality" : v === "trojan" ? "tls" : "", flow: v === "vless" ? "xtls-rprx-vision" : "" })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(protocolLabels).map(([v, l]) => (
                    <SelectItem key={v} value={v}>
                      {l}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field id="px-port" label="Порт">
              <Input id="px-port" inputMode="numeric" value={String(proxy.port || "")} onChange={(e) => set({ port: Number(e.target.value.replace(/\D/g, "")) })} />
            </Field>
          </div>
          <Field id="px-address" label="Адрес" hint="публичный домен или IP; внутренние адреса ядро не пропустит">
            <Input id="px-address" value={proxy.address} onChange={(e) => set({ address: e.target.value })} placeholder="nl.example.com" />
          </Field>

          {proxy.protocol === "vless" && (
            <>
              <Field id="px-uuid" label="UUID" hint={hasSecret ? "сохранён — оставь пустым; если поменяешь в выходе что-то ещё, введи заново" : undefined}>
                <Input id="px-uuid" value={proxy.uuid ?? ""} onChange={(e) => set({ uuid: e.target.value })} placeholder={hasSecret ? "••••••••" : "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"} className="font-mono text-xs" />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Защита">
                  <Select value={proxy.security || "reality"} onValueChange={(v) => set({ security: v })}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="reality">REALITY</SelectItem>
                      <SelectItem value="tls">TLS</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <div className="well flex items-center justify-between gap-3 rounded-xl p-3">
                  <div className="text-sm">flow vision</div>
                  <Switch checked={proxy.flow === "xtls-rprx-vision"} onCheckedChange={(v) => set({ flow: v ? "xtls-rprx-vision" : "" })} />
                </div>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field id="px-sni" label="SNI" hint={proxy.security === "reality" ? "обязателен" : "пусто — по адресу"}>
                  <Input id="px-sni" value={proxy.sni ?? ""} onChange={(e) => set({ sni: e.target.value })} />
                </Field>
                <Field label="Отпечаток">
                  <FingerprintSelect value={proxy.fingerprint ?? ""} onChange={(v) => set({ fingerprint: v })} />
                </Field>
              </div>
              {proxy.security === "reality" && (
                <div className="grid gap-4 sm:grid-cols-[1fr_9rem]">
                  <Field id="px-pbk" label="Публичный ключ">
                    <Input id="px-pbk" value={proxy.public_key ?? ""} onChange={(e) => set({ public_key: e.target.value })} className="font-mono text-xs" />
                  </Field>
                  <Field id="px-sid" label="shortId">
                    <Input id="px-sid" value={proxy.short_id ?? ""} onChange={(e) => set({ short_id: e.target.value })} className="font-mono text-xs" />
                  </Field>
                </div>
              )}
            </>
          )}

          {proxy.protocol === "trojan" && (
            <>
              <SecretField hasSecret={hasSecret} value={proxy.password ?? ""} onChange={(v) => set({ password: v })} />
              <div className="grid gap-4 sm:grid-cols-2">
                <Field id="px-sni" label="SNI" hint="пусто — по адресу">
                  <Input id="px-sni" value={proxy.sni ?? ""} onChange={(e) => set({ sni: e.target.value })} />
                </Field>
                <Field label="Отпечаток">
                  <FingerprintSelect value={proxy.fingerprint ?? ""} onChange={(v) => set({ fingerprint: v })} />
                </Field>
              </div>
            </>
          )}

          {proxy.protocol === "shadowsocks" && (
            <>
              <Field label="Метод">
                <Select value={proxy.method ?? ""} onValueChange={(v) => set({ method: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder="выбери" />
                  </SelectTrigger>
                  <SelectContent>
                    {ssMethods.map((m) => (
                      <SelectItem key={m} value={m}>
                        {m}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <SecretField hasSecret={hasSecret} value={proxy.password ?? ""} onChange={(v) => set({ password: v })} />
            </>
          )}

          {(proxy.protocol === "socks" || proxy.protocol === "http") && (
            <>
              {proxy.protocol === "http" && (
                <div className="well flex items-center justify-between gap-3 rounded-xl p-3">
                  <div>
                    <div className="text-sm font-medium">TLS</div>
                    <div className="text-xs text-muted-foreground">HTTPS-прокси</div>
                  </div>
                  <Switch checked={proxy.security === "tls"} onCheckedChange={(v) => set({ security: v ? "tls" : "" })} />
                </div>
              )}
              <div className="grid gap-4 sm:grid-cols-2">
                <Field id="px-user" label="Логин" hint="необязательно">
                  <Input id="px-user" value={proxy.username ?? ""} onChange={(e) => set({ username: e.target.value })} autoComplete="off" />
                </Field>
                <SecretField hasSecret={hasSecret} value={proxy.password ?? ""} onChange={(v) => set({ password: v })} optional />
              </div>
            </>
          )}
        </>
      )}

      {kind === "cascade" && (
        <Field label="Нода-выход" hint="трафик пойдёт на неё по VLESS + REALITY служебным ключом и выйдет в интернет там; ноды этого профиля сюда не попадают — каскад на себя нельзя">
          <Select value={nodeId} onValueChange={setNodeId}>
            <SelectTrigger>
              <SelectValue placeholder="выбери ноду" />
            </SelectTrigger>
            <SelectContent>
              {candidates.map((n) => (
                <SelectItem key={n.id} value={String(n.id)}>
                  {n.name}
                  {!n.enabled && " · выключена"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      )}

      {kind === "raw" && (
        <Field id="ob-raw" label="outbound xray" hint="тег подставится сам; секреты показаны как *** — не трогай их, и они сохранятся, а изменённый выход требует ввести их заново">
          <Textarea id="ob-raw" value={raw} onChange={(e) => setRaw(e.target.value)} rows={10} className="font-mono text-xs" placeholder={'{\n  "protocol": "wireguard",\n  "settings": { … }\n}'} />
        </Field>
      )}
    </ActionDialog>
  )
}

function FingerprintSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Select value={value || "chrome"} onValueChange={onChange}>
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {fingerprints.map((f) => (
          <SelectItem key={f} value={f}>
            {f}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function SecretField({ hasSecret, value, onChange, optional }: { hasSecret: boolean; value: string; onChange: (v: string) => void; optional?: boolean }) {
  return (
    <Field id="px-pass" label="Пароль" hint={hasSecret ? "сохранён — оставь пустым; если поменяешь в выходе что-то ещё, введи заново" : optional ? "вместе с логином" : undefined}>
      <Input id="px-pass" type="password" value={value} onChange={(e) => onChange(e.target.value)} placeholder={hasSecret ? "••••••••" : undefined} autoComplete="new-password" />
    </Field>
  )
}
