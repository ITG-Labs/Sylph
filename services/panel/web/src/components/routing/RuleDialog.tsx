import { useEffect, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { sniffProtocols, targets, type GeoSource, type Rule, type RoutingDoc } from "@/lib/routing"
import type { Plan } from "@/lib/types"
import { CheckChip, Field, splitList } from "./parts"

const networks = [
  { value: "any", label: "любая" },
  { value: "tcp", label: "TCP" },
  { value: "udp", label: "UDP" },
  { value: "tcp,udp", label: "TCP и UDP" },
]

export function RuleDialog({
  open,
  onOpenChange,
  doc,
  rule,
  plans,
  sources,
  onSave,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  doc: RoutingDoc
  rule?: Rule
  plans: Plan[]
  sources: GeoSource[]
  onSave: (r: Rule) => void
}) {
  const [mode, setMode] = useState<"match" | "plan">("match")
  const [name, setName] = useState("")
  const [domain, setDomain] = useState("")
  const [ip, setIp] = useState("")
  const [port, setPort] = useState("")
  const [network, setNetwork] = useState("any")
  const [protocol, setProtocol] = useState<string[]>([])
  const [inbound, setInbound] = useState("")
  const [planIds, setPlanIds] = useState<number[]>([])
  const [to, setTo] = useState("")

  useEffect(() => {
    if (!open) return
    setMode(rule?.plans?.length ? "plan" : "match")
    setName(rule?.name ?? "")
    setDomain(rule?.domain?.join("\n") ?? "")
    setIp(rule?.ip?.join("\n") ?? "")
    setPort(rule?.port ?? "")
    setNetwork(rule?.network || "any")
    setProtocol(rule?.protocol ?? [])
    setInbound(rule?.inbound?.join(", ") ?? "")
    setPlanIds(rule?.plans ?? [])
    setTo(rule?.to ?? "")
  }, [open, rule])

  const all = targets(doc)
  const built: Rule =
    mode === "plan"
      ? { ...(name.trim() ? { name: name.trim() } : {}), plans: planIds, to }
      : {
          ...(name.trim() ? { name: name.trim() } : {}),
          ...(splitList(domain).length ? { domain: splitList(domain) } : {}),
          ...(splitList(ip).length ? { ip: splitList(ip) } : {}),
          ...(port.trim() ? { port: port.replace(/\s+/g, "") } : {}),
          ...(network !== "any" ? { network } : {}),
          ...(protocol.length ? { protocol } : {}),
          ...(splitList(inbound).length ? { inbound: splitList(inbound) } : {}),
          to,
        }
  const conditions = mode === "plan" ? planIds.length > 0 : Object.keys(built).some((k) => k !== "name" && k !== "to")
  const problem = !conditions ? (mode === "plan" ? "отметь хотя бы один тариф" : "нужно хотя бы одно условие") : !all.some((t) => t.value === to) ? "выбери, куда отправлять" : null
  const domainSources = sources.filter((s) => s.kind === "domain")
  const ipSources = sources.filter((s) => s.kind === "ip")

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={rule ? "Правило" : "Новое правило"}
      description={mode === "plan" ? "юзеры этих тарифов на ноде — правило держится в работающем xray, смена состава без перезапуска" : "все условия правила должны совпасть разом; внутри списка — любое значение"}
      consequencesBelow
      consequences={problem ? <p>{problem}</p> : undefined}
      confirmLabel={rule ? "Готово" : "Добавить"}
      canConfirm={!problem}
      onConfirm={async () => onSave(built)}
    >
      <Tabs value={mode} onValueChange={(v) => setMode(v as "match" | "plan")}>
        <TabsList className="w-full">
          <TabsTrigger value="match">По условиям</TabsTrigger>
          <TabsTrigger value="plan">По тарифу</TabsTrigger>
        </TabsList>
      </Tabs>
      <Field id="rl-name" label="Название" hint="необязательно — чтобы помнить, зачем правило">
        <Input id="rl-name" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} placeholder={mode === "plan" ? "Премиум через Нидерланды" : "Блок торрентов"} />
      </Field>

      {mode === "match" ? (
        <>
          <Field
            id="rl-domain"
            label="Домены"
            hint={
              <>
                example.com — вместе с поддоменами; full:, keyword:, regexp:; geosite:тег из образа xray
                {domainSources.length > 0 && (
                  <span className="mt-1.5 flex flex-wrap items-center gap-1">
                    свои базы:
                    {domainSources.map((s) => (
                      <Badge key={s.id} variant="outline" className="font-mono">
                        {s.name}:тег
                      </Badge>
                    ))}
                  </span>
                )}
              </>
            }
          >
            <Textarea id="rl-domain" value={domain} onChange={(e) => setDomain(e.target.value)} rows={3} className="font-mono text-xs" placeholder={"geosite:category-ads-all\nexample.com"} />
          </Field>
          <Field
            id="rl-ip"
            label="IP"
            hint={
              <>
                адреса и подсети, geoip:тег из образа xray
                {ipSources.length > 0 && (
                  <span className="mt-1.5 flex flex-wrap items-center gap-1">
                    свои базы:
                    {ipSources.map((s) => (
                      <Badge key={s.id} variant="outline" className="font-mono">
                        {s.name}:тег
                      </Badge>
                    ))}
                  </span>
                )}
              </>
            }
          >
            <Textarea id="rl-ip" value={ip} onChange={(e) => setIp(e.target.value)} rows={2} className="font-mono text-xs" placeholder={"geoip:ru\n1.1.1.0/24"} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field id="rl-port" label="Порты назначения" hint="443 или 1000-2000, через запятую">
              <Input id="rl-port" value={port} onChange={(e) => setPort(e.target.value)} className="font-mono text-xs" />
            </Field>
            <Field label="Сеть">
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
          <Field label="Протокол" hint="xray узнаёт его по первым байтам соединения">
            <div className="flex flex-wrap gap-2">
              {sniffProtocols.map((p) => (
                <CheckChip key={p.value} checked={protocol.includes(p.value)} onChange={(v) => setProtocol((x) => (v ? [...x, p.value] : x.filter((y) => y !== p.value)))}>
                  {p.label}
                </CheckChip>
              ))}
            </div>
          </Field>
          <Field id="rl-inbound" label="Вход ноды" hint="теги входов через запятую — правило только для пришедших на них">
            <Input id="rl-inbound" value={inbound} onChange={(e) => setInbound(e.target.value)} className="font-mono text-xs" />
          </Field>
        </>
      ) : (
        <Field label="Тарифы" hint="правила по тарифу всегда идут после остальных">
          <div className="flex flex-wrap gap-2">
            {plans.map((p) => (
              <CheckChip key={p.id} checked={planIds.includes(p.id)} onChange={(v) => setPlanIds((x) => (v ? [...x, p.id] : x.filter((y) => y !== p.id)))}>
                {p.name}
              </CheckChip>
            ))}
          </div>
        </Field>
      )}

      <Field label="Куда отправлять">
        <Select value={to} onValueChange={setTo}>
          <SelectTrigger>
            <SelectValue placeholder="выбери выход или балансировщик" />
          </SelectTrigger>
          <SelectContent>
            {all.map((t) => (
              <SelectItem key={t.value} value={t.value}>
                {t.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
    </ActionDialog>
  )
}
