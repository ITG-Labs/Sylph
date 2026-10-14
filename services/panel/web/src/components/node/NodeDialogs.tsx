import { useEffect, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { MoveConsequences } from "@/components/group/MoveConsequences"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { core } from "@/lib/api"
import { describeNodeMove, isHarsh, nodeMoveImpact, type ImpactLine } from "@/lib/groups"
import { useNodes, usePlans } from "@/lib/queries"
import type { Group, Inbound, Node } from "@/lib/types"

export const fingerprints = ["chrome", "firefox", "safari", "ios", "android", "edge", "random"]

function Field({ id, label, hint, children }: { id?: string; label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

function GroupSelect({ groups, value, onChange }: { groups: Group[]; value: string; onChange: (v: string) => void }) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="none">Без группы — видна всем тарифам</SelectItem>
        {groups.map((g) => (
          <SelectItem key={g.id} value={String(g.id)}>
            {g.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

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
  const [group, setGroup] = useState("none")
  const [enabled, setEnabled] = useState(true)

  useEffect(() => {
    if (!open) return
    setCn(node?.cert_cn ?? "")
    setName(node?.name ?? "")
    setAddress(node?.address ?? "")
    setGroup(node?.group_id ? String(node.group_id) : "none")
    setEnabled(node?.enabled ?? true)
  }, [open, node])

  const groupID = group === "none" ? null : Number(group)
  const ok = name.trim() !== "" && address.trim() !== "" && (node || cn.trim() !== "")
  const plans = usePlans()
  const nodes = useNodes()
  const moveLines: ImpactLine[] =
    node && groupID !== node.group_id && plans.data && nodes.data
      ? describeNodeMove(
          { ...node, enabled },
          groupID,
          nodeMoveImpact(
            { ...node, enabled },
            groupID,
            plans.data.map((p) => ({ ...p, active_users: p.active_users ?? 0 })),
            nodes.data,
          ),
        )
      : []
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
          ? await core.patch<Node>(`/v1/admin/nodes/${node.id}`, { name: name.trim(), address: address.trim(), group_id: groupID, enabled })
          : await core.post<Node>("/v1/admin/nodes", { cert_cn: cn.trim(), name: name.trim(), address: address.trim(), group_id: groupID, enabled })
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
      <Field label="Группа локаций">
        <GroupSelect groups={groups} value={group} onChange={setGroup} />
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
  inbound,
  onDone,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  nodeId: number
  inbound?: Inbound
  onDone: () => void
}) {
  const [tag, setTag] = useState("")
  const [label, setLabel] = useState("")
  const [dest, setDest] = useState("")
  const [sni, setSni] = useState("")
  const [sids, setSids] = useState("")
  const [fp, setFp] = useState("chrome")

  useEffect(() => {
    if (!open) return
    setTag(inbound?.tag ?? "reality-tcp")
    setLabel(inbound?.label ?? "")
    setDest(inbound?.reality.dest ?? "")
    setSni(inbound?.reality.server_names.join("\n") ?? "")
    setSids(inbound?.reality.short_ids.join("\n") ?? "")
    setFp(inbound?.reality.fingerprint ?? "chrome")
  }, [open, inbound])

  const ok = tag.trim() !== "" && dest.trim() !== "" && lines(sni).length > 0

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={inbound ? `Изменить вход ${inbound.tag}` : "Новый вход REALITY"}
      description="VLESS + TCP + REALITY, flow xtls-rprx-vision, порт 443"
      consequences={inbound ? <p>Агент пересоберёт конфиг xray и перезапустит его — открытые соединения ноды оборвутся. Юзеры получат новые параметры со следующим обновлением подписки.</p> : undefined}
      confirmLabel={inbound ? "Сохранить" : "Создать вход"}
      canConfirm={ok}
      success={inbound ? "Вход сохранён" : "Вход создан"}
      onConfirm={async () => {
        const reality: Record<string, unknown> = { dest: dest.trim(), server_names: lines(sni), fingerprint: fp }
        if (lines(sids).length > 0) reality.short_ids = lines(sids)
        if (inbound) await core.patch(`/v1/admin/inbounds/${inbound.id}`, { tag: tag.trim(), label: label.trim(), reality })
        else await core.post(`/v1/admin/nodes/${nodeId}/inbounds`, { tag: tag.trim(), port: 443, label: label.trim(), reality })
        onDone()
      }}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field id="tag" label="Тег" hint="a-z, 0-9, дефис; уникален на ноде">
          <Input id="tag" value={tag} onChange={(e) => setTag(e.target.value)} />
        </Field>
        <Field id="label" label="Подпись для юзера" hint="«Германия · REALITY», если входов несколько">
          <Input id="label" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="REALITY" />
        </Field>
      </div>
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
          <Select value={fp} onValueChange={setFp}>
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
        </Field>
      </div>
    </ActionDialog>
  )
}
