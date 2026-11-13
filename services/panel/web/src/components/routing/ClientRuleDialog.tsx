import { useEffect, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { actionLabels, normalizeRule, type ClientAction, type ClientRule, type GeoTags } from "@/lib/clientRouting"
import type { Node } from "@/lib/types"
import { Field } from "./parts"

const lines = (s: string) => s.split("\n").map((x) => x.trim()).filter(Boolean)

const networks = [
  { value: "any", label: "любая" },
  { value: "tcp", label: "TCP" },
  { value: "udp", label: "UDP" },
]

export function ClientRuleDialog({
  open,
  onOpenChange,
  rule,
  nodes,
  tags,
  onSave,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  rule?: ClientRule
  nodes: Node[]
  tags?: GeoTags
  onSave: (r: ClientRule) => void
}) {
  const [name, setName] = useState("")
  const [action, setAction] = useState<ClientAction>("direct")
  const [nodeId, setNodeId] = useState("")
  const [domain, setDomain] = useState("")
  const [ip, setIp] = useState("")
  const [port, setPort] = useState("")
  const [network, setNetwork] = useState("any")
  const [search, setSearch] = useState("")

  useEffect(() => {
    if (!open) return
    setName(rule?.name ?? "")
    setAction(rule?.action ?? "direct")
    setNodeId(rule?.node_id ? String(rule.node_id) : "")
    setDomain(rule?.domain?.join("\n") ?? "")
    setIp(rule?.ip?.join("\n") ?? "")
    setPort(rule?.port ?? "")
    setNetwork(rule?.network || "any")
    setSearch("")
  }, [open, rule])

  const known = !!tags && (tags.geosite.some((g) => `geosite:${g}` === search.trim()) || tags.geoip.some((g) => `geoip:${g}` === search.trim()))
  const addCategory = () => {
    const v = search.trim()
    if (v.startsWith("geosite:")) setDomain((d) => (d.trim() ? `${d.trimEnd()}\n${v}` : v))
    else setIp((d) => (d.trim() ? `${d.trimEnd()}\n${v}` : v))
    setSearch("")
  }

  const built = normalizeRule({
    name,
    action,
    node_id: action === "node" ? Number(nodeId) || undefined : undefined,
    domain: lines(domain),
    ip: lines(ip),
    port,
    network: network === "any" ? "" : network,
  })
  const hasDomain = (built.domain?.length ?? 0) > 0
  const hasIP = (built.ip?.length ?? 0) > 0
  const problem = !built.name
    ? "укажите название"
    : hasDomain && hasIP
      ? "в одном правиле либо домены, либо IP — разделите на два правила"
      : !hasDomain && !hasIP && !built.port
        ? "нужны домены, IP или порт"
        : action === "node" && !built.node_id
          ? "выберите ноду"
          : null

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={rule ? "Правило" : "Новое правило"}
      description="условия правила выполняются одновременно; внутри списка достаточно совпадения любого значения"
      consequencesBelow
      consequences={problem ? <p>{problem}</p> : undefined}
      confirmLabel={rule ? "Готово" : "Добавить"}
      canConfirm={!problem}
      onConfirm={async () => onSave(built)}
    >
      <Field id="cr-name" label="Название" hint="видно пользователю в клиенте">
        <Input id="cr-name" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} placeholder="РФ напрямую" />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Действие">
          <Select value={action} onValueChange={(v) => setAction(v as ClientAction)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(actionLabels) as ClientAction[]).map((a) => (
                <SelectItem key={a} value={a}>
                  {actionLabels[a]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        {action === "node" && (
          <Field label="Нода" hint="клиент получит имя её подключения из подписки пользователя">
            <Select value={nodeId} onValueChange={setNodeId}>
              <SelectTrigger>
                <SelectValue placeholder="Выберите ноду" />
              </SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={String(n.id)}>
                    {n.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        )}
      </div>
      <Field id="cr-domain" label="Домены" hint="example.com или suffix:example.com — вместе с поддоменами; префиксы full:, keyword:, regexp:; geosite:категория из базы runetfreedom">
        <Textarea
          id="cr-domain"
          value={domain}
          onChange={(e) => setDomain(e.target.value)}
          disabled={hasIP}
          rows={3}
          className="font-mono text-xs"
          placeholder={"geosite:category-ru\ngosuslugi.ru"}
        />
      </Field>
      <Field id="cr-ip" label="IP" hint="адреса и подсети; geoip:код из базы runetfreedom">
        <Textarea id="cr-ip" value={ip} onChange={(e) => setIp(e.target.value)} disabled={hasDomain} rows={2} className="font-mono text-xs" placeholder={"geoip:ru\n10.0.0.0/8"} />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field id="cr-port" label="Порты" hint="443, 80,443 или 1000-2000; Happ и SimpleXray такие правила пропустят">
          <Input id="cr-port" value={port} onChange={(e) => setPort(e.target.value)} className="font-mono text-xs" />
        </Field>
        <Field label="Сеть" hint="понимает только ITG Ray">
          <Select value={network} onValueChange={setNetwork}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {networks.map((n) => (
                <SelectItem key={n.value} value={n.value}>
                  {n.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      {tags && tags.geosite.length > 0 && (
        <Field id="cr-cat" label="Категории runetfreedom" hint="начните вводить geosite: или geoip:, выберите категорию и добавьте её в правило">
          <div className="flex gap-2">
            <Input id="cr-cat" list="cr-categories" value={search} onChange={(e) => setSearch(e.target.value)} className="font-mono text-xs" placeholder="geosite:category-" />
            <Button type="button" variant="secondary" disabled={!known || (search.trim().startsWith("geosite:") ? hasIP : hasDomain)} onClick={addCategory}>
              Добавить
            </Button>
          </div>
          <datalist id="cr-categories">
            {tags.geosite.map((g) => (
              <option key={`s-${g}`} value={`geosite:${g}`} />
            ))}
            {tags.geoip.map((g) => (
              <option key={`i-${g}`} value={`geoip:${g}`} />
            ))}
          </datalist>
        </Field>
      )}
    </ActionDialog>
  )
}
