import { useEffect, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { MoveConsequences } from "@/components/group/MoveConsequences"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { core } from "@/lib/api"
import { CheckList } from "@/components/common/CheckList"
import { accessChanges, describeChanges, isHarsh, newOldAgentLines, withNodeGroups, type ImpactLine } from "@/lib/groups"
import { useWorld } from "@/lib/world"
import type { Group, Inbound, Node } from "@/lib/types"
import { agentKnowsXHTTP, extraToForm, formToExtra, kindHints, kindLabels, kindOf, minXrayForXHTTP, olderThan, type ExtraForm, type InboundKind } from "@/lib/inbounds"
import { ALPNPicker, ExtraFields, Field, ModeSelect } from "./XHTTPFields"

export const fingerprints = ["chrome", "firefox", "safari", "ios", "android", "edge", "random"]

function GroupChecks({ groups, value, onChange }: { groups: Group[]; value: number[]; onChange: (v: number[]) => void }) {
  if (groups.length === 0) return <p className="text-xs text-muted-foreground">Групп пока нет — нода будет видна всем тарифам.</p>
  return <CheckList label="Группы" items={groups.map((g) => ({ id: g.id, title: g.name }))} value={value} onChange={onChange} />
}

const sameIDs = (a: number[], b: number[]) => a.length === b.length && a.every((id) => b.includes(id))

export function NodeFormDialog({
  open,
  onOpenChange,
  node,
  groups,
  onDone,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  node?: Node
  groups: Group[]
  onDone: (n: Node) => void
}) {
  const [cn, setCn] = useState("")
  const [name, setName] = useState("")
  const [address, setAddress] = useState("")
  const [groupIDs, setGroupIDs] = useState<number[]>([])
  const [enabled, setEnabled] = useState(true)

  useEffect(() => {
    if (!open) return
    setCn(node?.cert_cn ?? "")
    setName(node?.name ?? "")
    setAddress(node?.address ?? "")
    setGroupIDs(node?.group_ids ?? [])
    setEnabled(node?.enabled ?? true)
  }, [open, node])

  const ok = name.trim() !== "" && address.trim() !== "" && (node || cn.trim() !== "")
  const { world } = useWorld()
  const regrouped = node && !sameIDs(groupIDs, node.group_ids) && world ? withNodeGroups(world, node.id, groupIDs) : null
  const moveLines: ImpactLine[] = regrouped && world ? [...describeChanges(world, accessChanges(world, regrouped)), ...newOldAgentLines(world, regrouped)] : []
  const disabling = !!node && node.enabled && !enabled

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={node ? `Изменить ноду «${node.name}»` : "Завести ноду"}
      description={node ? `CN серта: ${node.cert_cn} — не меняется` : "Серт ноды выпускается у тебя: make cert NAME=<CN> TYPE=client ROLE=node"}
      consequences={
        disabling || moveLines.length > 0 ? (
          <>
            {disabling && <p>Выключенная нода пропадёт из подписок со следующим обновлением, её юзеры переключатся на другие ноды.</p>}
            <MoveConsequences lines={moveLines} />
          </>
        ) : undefined
      }
      destructive={disabling || isHarsh(moveLines)}
      confirmLabel={node ? "Сохранить" : "Завести"}
      canConfirm={!!ok}
      success={node ? "Нода сохранена" : "Нода заведена"}
      onConfirm={async () => {
        const n = node
          ? await core.patch<Node>(`/v1/admin/nodes/${node.id}`, { name: name.trim(), address: address.trim(), group_ids: groupIDs, enabled })
          : await core.post<Node>("/v1/admin/nodes", { cert_cn: cn.trim(), name: name.trim(), address: address.trim(), group_ids: groupIDs, enabled })
        onDone(n)
      }}
    >
      {!node && (
        <Field id="cn" label="CN серта ноды" hint="латиница, цифры, точка, дефис, подчёркивание — как в серте">
          <Input id="cn" value={cn} onChange={(e) => setCn(e.target.value)} placeholder="node-de-1" autoFocus />
        </Field>
      )}
      <Field id="name" label="Имя" hint="его видят юзеры в клиенте">
        <Input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Германия" />
      </Field>
      <Field id="address" label="Адрес" hint="домен или IP, на который ходят клиенты">
        <Input id="address" value={address} onChange={(e) => setAddress(e.target.value)} placeholder="de1.example.com" />
      </Field>
      <Field label="Группы" hint="нода целиком; без групп её видят все тарифы. Отдельные входы раскладываются по группам на экране группы">
        <GroupChecks groups={groups} value={groupIDs} onChange={setGroupIDs} />
      </Field>
      <div className="well flex items-center justify-between gap-3 rounded-xl p-3">
        <div>
          <div className="text-sm font-medium">Включена</div>
          <div className="text-xs text-muted-foreground">выключенная не попадает в подписки</div>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>
    </ActionDialog>
  )
}

function lines(s: string) {
  return s
    .split(/[\s,]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

export function InboundFormDialog({
  open,
  onOpenChange,
  nodeId,
  node,
  inbound,
  onDone,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  nodeId: number
  node?: Node
  inbound?: Inbound
  onDone: () => void
}) {
  const [kind, setKind] = useState<InboundKind>("reality-tcp")
  const [tag, setTag] = useState("")
  const [label, setLabel] = useState("")
  const [dest, setDest] = useState("")
  const [sni, setSni] = useState("")
  const [sids, setSids] = useState("")
  const [fp, setFp] = useState("chrome")
  const [path, setPath] = useState("")
  const [mode, setMode] = useState("auto")
  const [clientMode, setClientMode] = useState("auto")
  const [extra, setExtra] = useState<ExtraForm>(extraToForm({}))
  const [domain, setDomain] = useState("")
  const [decoy, setDecoy] = useState("")
  const [address, setAddress] = useState("")
  const [port, setPort] = useState("443")
  const [clientSNI, setClientSNI] = useState("")
  const [host, setHost] = useState("")
  const [alpn, setAlpn] = useState<string[]>([])
  const [tlsFp, setTlsFp] = useState("chrome")

  useEffect(() => {
    if (!open) return
    const k = inbound ? kindOf(inbound) : "reality-tcp"
    setKind(k)
    setTag(inbound?.tag ?? "reality-tcp")
    setLabel(inbound?.label ?? "")
    setDest(inbound?.reality?.dest ?? "")
    setSni(inbound?.reality?.server_names.join("\n") ?? "")
    setSids(inbound?.reality?.short_ids.join("\n") ?? "")
    setFp(inbound?.reality?.fingerprint ?? "chrome")
    setPath(inbound?.xhttp?.path ?? "")
    setMode(inbound?.xhttp?.mode ?? "auto")
    setClientMode(inbound?.xhttp?.client_mode ?? "auto")
    setExtra(extraToForm(inbound?.xhttp?.extra))
    setDomain(inbound?.tls?.domain ?? "")
    setDecoy(inbound?.tls?.decoy ?? "")
    setAddress(inbound?.tls?.address ?? "")
    setPort(String(inbound?.tls?.port ?? 443))
    setClientSNI(inbound?.tls?.sni ?? "")
    setHost(inbound?.tls?.host ?? "")
    setAlpn(inbound?.tls?.alpn ?? [])
    setTlsFp(inbound?.tls?.fingerprint ?? "chrome")
  }, [open, inbound])

  const pickKind = (k: InboundKind) => {
    setKind(k)
    if (!inbound && Object.values(defaultTags).includes(tag)) setTag(defaultTags[k])
  }

  const reality = kind !== "tls-xhttp"
  const xhttp = kind !== "reality-tcp"
  const tls = kind === "tls-xhttp"
  const agentBehind = xhttp && !!node && !agentKnowsXHTTP(node)
  const xrayBehind = xhttp && !!node && olderThan(node.xray_version, minXrayForXHTTP)
  const ok =
    tag.trim() !== "" &&
    (!reality || (dest.trim() !== "" && lines(sni).length > 0)) &&
    (!xhttp || path.trim().startsWith("/")) &&
    (!tls || domain.trim() !== "") &&
    !agentBehind

  const consequences = (
    <>
      {agentBehind && <p className="font-medium">Агент этой ноды ещё не знает XHTTP — сначала обнови агента (sylph update на ноде).</p>}
      {xrayBehind && (
        <p>
          xray на ноде {node?.xray_version} старее {minXrayForXHTTP} — новые поля XHTTP он может молча пропустить, и клиенты не подключатся. Подними XRAY_VERSION в .env ноды.
        </p>
      )}
      {tls && !inbound && (
        <p>
          Направь DNS домена {domain.trim() || "…"} на ноду{node?.address ? ` (${node.address})` : ""}. Серт Let's Encrypt нода выпустит сама через 443. Для CDN укажи этот домен как origin.
        </p>
      )}
      {inbound && <p>Агент пересоберёт конфиг xray и перезапустит его — открытые соединения ноды оборвутся. Юзеры получат новые параметры со следующим обновлением подписки.</p>}
    </>
  )
  const hasConsequences = agentBehind || xrayBehind || (tls && !inbound) || !!inbound

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      wide={xhttp}
      title={inbound ? `Изменить вход ${inbound.tag}` : "Новый вход"}
      description={kindHints[kind]}
      consequences={hasConsequences ? consequences : undefined}
      consequencesBelow
      confirmLabel={inbound ? "Сохранить" : "Создать вход"}
      canConfirm={ok}
      success={inbound ? "Вход сохранён" : "Вход создан"}
      onConfirm={async () => {
        const body: Record<string, unknown> = { tag: tag.trim(), label: label.trim() }
        if (reality) {
          const r: Record<string, unknown> = { dest: dest.trim(), server_names: lines(sni), fingerprint: fp }
          if (lines(sids).length > 0) r.short_ids = lines(sids)
          body.reality = r
        }
        if (xhttp) body.xhttp = { path: path.trim(), mode, client_mode: clientMode, extra: formToExtra(extra) }
        if (tls) {
          body.tls = {
            domain: domain.trim(),
            decoy: decoy.trim(),
            address: address.trim(),
            port: Number(port) || 0,
            sni: clientSNI.trim(),
            host: host.trim(),
            alpn,
            fingerprint: tlsFp,
          }
        }
        if (inbound) await core.patch(`/v1/admin/inbounds/${inbound.id}`, body)
        else await core.post(`/v1/admin/nodes/${nodeId}/inbounds`, { ...body, port: 443, transport: xhttp ? "xhttp" : "tcp", security: tls ? "tls" : "reality" })
        onDone()
      }}
    >
      {!inbound && (
        <Field label="Вид входа">
          <Select value={kind} onValueChange={(v) => pickKind(v as InboundKind)}>
            <SelectTrigger aria-label="Вид входа">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(kindLabels) as InboundKind[]).map((k) => (
                <SelectItem key={k} value={k}>
                  {kindLabels[k]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field id="tag" label="Тег" hint="a-z, 0-9, дефис; уникален на ноде">
          <Input id="tag" value={tag} onChange={(e) => setTag(e.target.value)} />
        </Field>
        <Field id="label" label="Подпись для юзера" hint="«Германия · CDN», если входов несколько">
          <Input id="label" value={label} onChange={(e) => setLabel(e.target.value)} placeholder={tls ? "CDN" : "REALITY"} />
        </Field>
      </div>
      {reality && (
        <>
          <Field id="dest" label="Под какой сайт маскируемся (dest)" hint="host:port настоящего сайта; не localhost, не внутренние адреса и не наши ноды">
            <Input id="dest" value={dest} onChange={(e) => setDest(e.target.value)} placeholder="www.microsoft.com:443" />
          </Field>
          <Field id="sni" label="SNI (server names)" hint="по одному в строке; уникальны среди входов ноды">
            <Textarea id="sni" value={sni} onChange={(e) => setSni(e.target.value)} placeholder="www.microsoft.com" rows={2} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field id="sids" label="shortId" hint={inbound ? "hex, 2–16 знаков; пусто — оставить как есть" : "hex, 2–16 знаков; пусто — ядро сгенерирует"}>
              <Textarea id="sids" value={sids} onChange={(e) => setSids(e.target.value)} rows={2} className="font-mono" />
            </Field>
            <Field label="Отпечаток браузера" hint="везде работает только chrome">
              <FingerprintSelect value={fp} onChange={setFp} />
            </Field>
          </div>
        </>
      )}
      {tls && (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="domain" label="Домен ноды" hint="на него смотрит DNS и origin в CDN; на него нода выпускает серт">
            <Input id="domain" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="static.example.ru" />
          </Field>
          <Field id="decoy" label="Маскировка" hint="сайт, который видят на чужих путях и ошибках; пусто — простая заглушка">
            <Input id="decoy" value={decoy} onChange={(e) => setDecoy(e.target.value)} placeholder="https://example.com" />
          </Field>
        </div>
      )}
      {xhttp && (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="sm:col-span-3">
              <Field id="path" label="Путь" hint="одинаковый у сервера и клиента; у входов на одном домене пути не пересекаются">
                <Input id="path" value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/v2/catalog/sync.json" className="font-mono" />
              </Field>
            </div>
            <Field id="mode" label="mode сервера" hint="auto принимает всё">
              <ModeSelect id="mode" value={mode} onChange={setMode} />
            </Field>
            <Field id="client-mode" label="mode клиента" hint="через CDN обычно packet-up">
              <ModeSelect id="client-mode" value={clientMode} onChange={setClientMode} />
            </Field>
          </div>
          <ExtraFields form={extra} onChange={setExtra} />
        </>
      )}
      {tls && (
        <div className="well flex flex-col gap-4 rounded-xl p-3">
          <div>
            <div className="text-sm font-medium">Как подключаются клиенты</div>
            <div className="text-xs text-muted-foreground">для CDN — IP или домен edge и домен ресурса CDN; без CDN оставь пустым</div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field id="address" label="Адрес" hint="пусто — домен ноды">
              <Input id="address" value={address} onChange={(e) => setAddress(e.target.value)} placeholder="185.31.114.248" className="font-mono" />
            </Field>
            <Field id="client-port" label="Порт">
              <Input id="client-port" value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" className="font-mono" />
            </Field>
            <Field id="client-sni" label="SNI" hint="пусто — адрес, если он домен, иначе домен ноды">
              <Input id="client-sni" value={clientSNI} onChange={(e) => setClientSNI(e.target.value)} placeholder="xxx.cdn.example.ru" className="font-mono" />
            </Field>
            <Field id="client-host" label="Host" hint="пусто — как SNI">
              <Input id="client-host" value={host} onChange={(e) => setHost(e.target.value)} className="font-mono" />
            </Field>
            <Field label="ALPN" hint="ничего не выбрано — как решит клиент">
              <ALPNPicker value={alpn} onChange={setAlpn} />
            </Field>
            <Field label="Отпечаток браузера">
              <FingerprintSelect value={tlsFp} onChange={setTlsFp} />
            </Field>
          </div>
        </div>
      )}
    </ActionDialog>
  )
}

const defaultTags: Record<InboundKind, string> = { "reality-tcp": "reality-tcp", "reality-xhttp": "reality-xhttp", "tls-xhttp": "cdn" }

function FingerprintSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Select value={value} onValueChange={onChange}>
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
