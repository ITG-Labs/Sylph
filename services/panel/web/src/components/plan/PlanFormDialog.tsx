import { useEffect, useState } from "react"
import { Plus, X } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Button } from "@/components/ui/button"
import { MoveConsequences } from "@/components/group/MoveConsequences"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { core } from "@/lib/api"
import { parseGB } from "@/lib/format"
import { accessChanges, describeChanges, isHarsh, withPlanGroup, type ImpactLine } from "@/lib/groups"
import { useNodes } from "@/lib/queries"
import type { Group, Plan, PlanPool } from "@/lib/types"
import { useWorld } from "@/lib/world"

const GiB = 1024 ** 3

function gbText(bytes: number | null | undefined) {
  return bytes ? String(Math.round((bytes / GiB) * 100) / 100) : ""
}

interface PoolDraft {
  id: number
  target: string
  name: string
  gb: string
}

const targetOf = (p: PlanPool) => (p.node_id !== null ? `n:${p.node_id}` : `g:${p.group_id}`)

function draftOf(p: PlanPool): PoolDraft {
  return { id: p.id, target: targetOf(p), name: p.name, gb: gbText(p.traffic_limit_bytes) }
}

function poolBody(d: PoolDraft) {
  const [kind, id] = d.target.split(":")
  return {
    ...(d.id ? { id: d.id } : {}),
    name: d.name.trim(),
    node_id: kind === "n" ? Number(id) : null,
    group_id: kind === "g" ? Number(id) : null,
    traffic_limit_bytes: d.gb.trim() === "" ? null : parseGB(d.gb),
  }
}

function PoolsEditor({ pools, onChange, groups }: { pools: PoolDraft[]; onChange: (p: PoolDraft[]) => void; groups: Group[] }) {
  const nodes = useNodes()
  const set = (i: number, patch: Partial<PoolDraft>) => onChange(pools.map((p, j) => (j === i ? { ...p, ...patch } : p)))
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <div>
          <Label>Отдельные пулы</Label>
          <p className="text-xs text-muted-foreground">трафик этих нод или групп считается отдельно, остальное — в основной лимит</p>
        </div>
        <Button type="button" size="sm" variant="secondary" onClick={() => onChange([...pools, { id: 0, target: "", name: "", gb: "" }])}>
          <Plus />
          Пул
        </Button>
      </div>
      {pools.map((p, i) => (
        <div key={i} className="well flex flex-col gap-2 rounded-xl p-3">
          <div className="flex items-center gap-2">
            <Select value={p.target} onValueChange={(v) => set(i, { target: v })}>
              <SelectTrigger aria-label="Что считать" className="min-w-0 flex-1">
                <SelectValue placeholder="нода или группа" />
              </SelectTrigger>
              <SelectContent>
                {(nodes.data ?? []).map((n) => (
                  <SelectItem key={`n${n.id}`} value={`n:${n.id}`}>
                    Нода «{n.name}»
                  </SelectItem>
                ))}
                {groups.map((g) => (
                  <SelectItem key={`g${g.id}`} value={`g:${g.id}`}>
                    Группа «{g.name}»
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="button" size="icon" variant="ghost" aria-label="Убрать пул" onClick={() => onChange(pools.filter((_, j) => j !== i))}>
              <X />
            </Button>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <Input aria-label="Название пула" value={p.name} onChange={(e) => set(i, { name: e.target.value })} maxLength={64} placeholder="название — как у цели" />
            <Input aria-label="Лимит пула, ГБ" inputMode="decimal" value={p.gb} onChange={(e) => set(i, { gb: e.target.value })} placeholder="ГБ, пусто — безлимит" />
          </div>
        </div>
      ))}
    </div>
  )
}

function Toggle({ title, hint, checked, onChange, disabled }: { title: string; hint: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <div className="well flex items-center justify-between gap-3 rounded-xl p-3">
      <div>
        <div className="text-sm font-medium">{title}</div>
        <div className="text-xs text-muted-foreground">{hint}</div>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} />
    </div>
  )
}

export function PlanFormDialog({
  open,
  onOpenChange,
  plan,
  groups,
  onDone,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  plan?: Plan
  groups: Group[]
  onDone: (p: Plan) => void
}) {
  const [name, setName] = useState("")
  const [price, setPrice] = useState("")
  const [days, setDays] = useState("")
  const [gb, setGb] = useState("")
  const [devices, setDevices] = useState("")
  const [hwid, setHwid] = useState(false)
  const [group, setGroup] = useState("")
  const [pools, setPools] = useState<PoolDraft[]>([])
  const [trial, setTrial] = useState(false)
  const [priv, setPriv] = useState(false)
  const [onSale, setOnSale] = useState(true)
  const [dedicated, setDedicated] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(plan?.name ?? "")
    setPrice(plan ? String(plan.price_rub) : "")
    setDays(plan ? String(plan.period_days) : "30")
    setGb(gbText(plan?.traffic_limit_bytes))
    setDevices(plan?.device_limit ? String(plan.device_limit) : "")
    setHwid(plan?.hwid_required ?? false)
    setGroup(plan ? String(plan.location_group_id) : "")
    setPools(plan?.pools.map(draftOf) ?? [])
    setTrial(plan?.trial ?? false)
    setPriv(plan?.visibility === "private")
    setOnSale(plan?.on_sale ?? true)
    setDedicated(plan?.dedicated_ip ?? false)
  }, [open, plan])

  const priceN = Number(price)
  const daysN = Number(days)
  const devN = devices === "" ? null : Number(devices)
  const bytes = gb.trim() === "" ? null : parseGB(gb)
  const valid =
    name.trim() !== "" &&
    price !== "" &&
    Number.isInteger(priceN) &&
    priceN >= 0 &&
    Number.isInteger(daysN) &&
    daysN >= 1 &&
    daysN <= 3650 &&
    (gb.trim() === "" || bytes !== null) &&
    (devN === null || (Number.isInteger(devN) && devN >= 1 && devN <= 100)) &&
    group !== "" &&
    pools.every((d) => d.target !== "" && (d.gb.trim() === "" || parseGB(d.gb) !== null))
  const hwidForced = devN !== null

  useEffect(() => {
    if (hwidForced) setHwid(true)
  }, [hwidForced])
  useEffect(() => {
    if (priceN !== 0) setTrial(false)
  }, [priceN])

  const groupID = Number(group)
  const poolsBody = pools.map(poolBody)
  const full: Record<string, unknown> = {
    name: name.trim(),
    price_rub: priceN,
    period_days: daysN,
    traffic_limit_bytes: bytes,
    device_limit: devN,
    hwid_required: hwid,
    location_group_id: groupID,
    trial,
    visibility: priv ? "private" : "public",
    on_sale: onSale,
    dedicated_ip: dedicated,
    pools: poolsBody,
  }
  const changed: Record<string, unknown> = {}
  if (plan) {
    if (full.name !== plan.name) changed.name = full.name
    if (priceN !== plan.price_rub) changed.price_rub = priceN
    if (daysN !== plan.period_days) changed.period_days = daysN
    if (gb.trim() !== gbText(plan.traffic_limit_bytes)) changed.traffic_limit_bytes = bytes
    if (devN !== plan.device_limit) changed.device_limit = devN
    if (hwid !== plan.hwid_required) changed.hwid_required = hwid
    if (groupID !== plan.location_group_id) changed.location_group_id = groupID
    if (trial !== plan.trial) changed.trial = trial
    if (full.visibility !== plan.visibility) changed.visibility = full.visibility
    if (onSale !== plan.on_sale) changed.on_sale = onSale
    if (dedicated !== plan.dedicated_ip) changed.dedicated_ip = dedicated
    if (JSON.stringify(poolsBody) !== JSON.stringify(plan.pools.map((p) => poolBody(draftOf(p))))) changed.pools = poolsBody
  }
  const nothingChanged = !!plan && Object.keys(changed).length === 0
  const { world } = useWorld()
  const moveLines: ImpactLine[] = plan && "location_group_id" in changed && world ? describeChanges(world, accessChanges(world, withPlanGroup(world, plan.id, groupID))) : []
  const limitLive = !!plan && ("traffic_limit_bytes" in changed || "pools" in changed)
  const dedicatedLive = !!plan && "dedicated_ip" in changed

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={plan ? `Тариф «${plan.name}»` : "Новый тариф"}
      consequences={
        limitLive || dedicatedLive || moveLines.length > 0 ? (
          <>
            {limitLive && <p>Новые лимиты действуют и на текущие периоды этого тарифа — ноды пересинкуются. Расход удалённого пула пропадёт, новый пул начнёт с нуля.</p>}
            {dedicatedLive && (
              <p>
                {dedicated
                  ? "Юзеры тарифа с закреплённым адресом начнут выходить через него — ноды с такими адресами перезапустят xray."
                  : "Закреплённые адреса юзеров тарифа перестанут работать, их трафик пойдёт через общие адреса — ноды с такими адресами перезапустят xray."}
              </p>
            )}
            <MoveConsequences lines={moveLines} />
          </>
        ) : undefined
      }
      destructive={isHarsh(moveLines)}
      confirmLabel={plan ? "Сохранить" : "Создать"}
      canConfirm={valid && !nothingChanged}
      success={plan ? "Тариф сохранён" : "Тариф создан"}
      onConfirm={async () => {
        const p = plan ? await core.patch<Plan>(`/v1/admin/plans/${plan.id}`, changed) : await core.post<Plan>("/v1/admin/plans", full)
        onDone(p)
      }}
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="pname">Название</Label>
        <Input id="pname" value={name} onChange={(e) => setName(e.target.value)} placeholder="Месяц" autoFocus={!plan} />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="price">Цена, ₽</Label>
          <Input id="price" inputMode="numeric" value={price} onChange={(e) => setPrice(e.target.value.replace(/\D/g, ""))} placeholder="199" />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pdays">Период, дней</Label>
          <Input id="pdays" inputMode="numeric" value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pgb">{pools.length > 0 ? "Основной лимит, ГБ" : "Трафик, ГБ"}</Label>
          <Input id="pgb" inputMode="decimal" value={gb} onChange={(e) => setGb(e.target.value)} placeholder="пусто — безлимит" />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pdev">Устройств</Label>
          <Input id="pdev" inputMode="numeric" value={devices} onChange={(e) => setDevices(e.target.value.replace(/\D/g, ""))} placeholder="пусто — без лимита" />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <Label>Группа</Label>
        {groups.length === 0 ? (
          <p className="text-xs text-muted-foreground">Сначала создай группу на экране «Группы» — тариф без группы завести нельзя.</p>
        ) : (
          <Select value={group} onValueChange={setGroup}>
            <SelectTrigger aria-label="Группа">
              <SelectValue placeholder="выбери группу" />
            </SelectTrigger>
            <SelectContent>
              {groups.map((g) => (
                <SelectItem key={g.id} value={String(g.id)}>
                  {g.name} + всё без группы
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      <PoolsEditor pools={pools} onChange={setPools} groups={groups} />
      <div className="flex flex-col gap-2">
        <Toggle title="Требовать HWID" hint={hwidForced ? "обязателен при лимите устройств" : "клиент без HWID получит заглушку"} checked={hwid} onChange={setHwid} disabled={hwidForced} />
        <Toggle title="Пробный" hint={priceN === 0 ? "выдаётся кнопкой в боте новичкам, раз на аккаунт" : "только для тарифа за 0 ₽"} checked={trial} onChange={setTrial} disabled={priceN !== 0} />
        <Toggle title="Приватный" hint="виден только по приглашению или доступу по Telegram ID" checked={priv} onChange={setPriv} />
        <Toggle title="В продаже" hint="снятый не покупается, но текущие могут продлить" checked={onSale} onChange={setOnSale} />
        <Toggle
          title="Выделенный IP"
          hint="юзер выходит через закреплённый за ним адрес на той ноде, где адрес есть; на остальных — через общие"
          checked={dedicated}
          onChange={setDedicated}
        />
      </div>
    </ActionDialog>
  )
}
