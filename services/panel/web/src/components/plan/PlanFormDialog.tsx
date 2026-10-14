import { useEffect, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { core } from "@/lib/api"
import { parseGB } from "@/lib/format"
import type { Group, Plan } from "@/lib/types"

const GiB = 1024 ** 3

function gbText(bytes: number | null | undefined) {
  return bytes ? String(Math.round((bytes / GiB) * 100) / 100) : ""
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
  const [group, setGroup] = useState("none")
  const [trial, setTrial] = useState(false)
  const [priv, setPriv] = useState(false)
  const [onSale, setOnSale] = useState(true)

  useEffect(() => {
    if (!open) return
    setName(plan?.name ?? "")
    setPrice(plan ? String(plan.price_rub) : "")
    setDays(plan ? String(plan.period_days) : "30")
    setGb(gbText(plan?.traffic_limit_bytes))
    setDevices(plan?.device_limit ? String(plan.device_limit) : "")
    setHwid(plan?.hwid_required ?? false)
    setGroup(plan?.location_group_id ? String(plan.location_group_id) : "none")
    setTrial(plan?.trial ?? false)
    setPriv(plan?.visibility === "private")
    setOnSale(plan?.on_sale ?? true)
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
    (devN === null || (Number.isInteger(devN) && devN >= 1 && devN <= 100))
  const hwidForced = devN !== null

  useEffect(() => {
    if (hwidForced) setHwid(true)
  }, [hwidForced])
  useEffect(() => {
    if (priceN !== 0) setTrial(false)
  }, [priceN])

  const groupID = group === "none" ? null : Number(group)
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
  }
  const nothingChanged = !!plan && Object.keys(changed).length === 0
  const changesLive = !!plan && ("traffic_limit_bytes" in changed || "location_group_id" in changed)

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={plan ? `Тариф «${plan.name}»` : "Новый тариф"}
      consequences={changesLive ? <p>Новые лимит трафика и группа нод действуют и на текущие периоды этого тарифа — ноды пересинкуются.</p> : undefined}
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
          <Label htmlFor="pgb">Трафик, ГБ</Label>
          <Input id="pgb" inputMode="decimal" value={gb} onChange={(e) => setGb(e.target.value)} placeholder="пусто — безлимит" />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pdev">Устройств</Label>
          <Input id="pdev" inputMode="numeric" value={devices} onChange={(e) => setDevices(e.target.value.replace(/\D/g, ""))} placeholder="пусто — без лимита" />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <Label>Группа нод</Label>
        <Select value={group} onValueChange={setGroup}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">Без группы — только общие ноды</SelectItem>
            {groups.map((g) => (
              <SelectItem key={g.id} value={String(g.id)}>
                {g.name} + общие ноды
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-2">
        <Toggle title="Требовать HWID" hint={hwidForced ? "обязателен при лимите устройств" : "клиент без HWID получит заглушку"} checked={hwid} onChange={setHwid} disabled={hwidForced} />
        <Toggle title="Пробный" hint={priceN === 0 ? "выдаётся кнопкой в боте новичкам, раз на аккаунт" : "только для тарифа за 0 ₽"} checked={trial} onChange={setTrial} disabled={priceN !== 0} />
        <Toggle title="Приватный" hint="виден только по приглашению или доступу по Telegram ID" checked={priv} onChange={setPriv} />
        <Toggle title="В продаже" hint="снятый не покупается, но текущие могут продлить" checked={onSale} onChange={setOnSale} />
      </div>
    </ActionDialog>
  )
}
