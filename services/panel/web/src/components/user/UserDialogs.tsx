import { useEffect, useMemo, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { core } from "@/lib/api"
import { fmtBytes, fmtDate, fmtPlanTraffic, fmtRub, parseGB } from "@/lib/format"
import { assignable } from "@/lib/addresses"
import type { NodeAddress, Period, Plan, UserAddress, UserCard } from "@/lib/types"

export type UserDialog =
  | { kind: "extend" }
  | { kind: "traffic" }
  | { kind: "plan" }
  | { kind: "manual" }
  | { kind: "rotate" }
  | { kind: "ban" }
  | { kind: "unban" }
  | { kind: "close"; period: Period }
  | { kind: "resetDevices" }
  | { kind: "deleteDevice"; deviceId: number; name: string }
  | { kind: "assignAddress" }
  | { kind: "unassignAddress"; address: UserAddress }

interface Props {
  dialog: UserDialog | null
  onClose: () => void
  card: UserCard
  plans: Plan[]
  addresses: NodeAddress[]
  onDone: () => void
}

function Chips({ values, onPick }: { values: { label: string; value: string }[]; onPick: (v: string) => void }) {
  return (
    <div className="flex flex-wrap gap-2">
      {values.map((v) => (
        <Button key={v.value} type="button" variant="secondary" size="sm" onClick={() => onPick(v.value)}>
          {v.label}
        </Button>
      ))}
    </div>
  )
}

function manualPaymentID() {
  const d = new Date()
  const ymd = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`
  const rnd = Array.from(crypto.getRandomValues(new Uint8Array(4)), (b) => b.toString(16).padStart(2, "0")).join("")
  return `manual-${ymd}-${rnd}`
}

export function UserDialogs({ dialog, onClose, card, plans, addresses, onDone }: Props) {
  const uid = card.user.id
  const current = card.periods.find((p) => p.state === "current")
  const open = (k: UserDialog["kind"]) => dialog?.kind === k
  const close = (v: boolean) => !v && onClose()
  const [days, setDays] = useState("")
  const [gb, setGb] = useState("")
  const [pool, setPool] = useState("")
  const [planId, setPlanId] = useState("")
  const [amount, setAmount] = useState("")
  const [addressId, setAddressId] = useState("")
  const [note, setNote] = useState("")
  const paymentId = useMemo(() => (dialog?.kind === "manual" ? manualPaymentID() : ""), [dialog])

  useEffect(() => {
    setDays("")
    setGb("")
    setPool(String(card.periods.find((p) => p.state === "current")?.pools.find((x) => x.traffic_limit_bytes !== null)?.pool_id ?? ""))
    setPlanId("")
    setAmount("")
    setAddressId("")
    setNote("")
  }, [dialog])

  const run = async (p: Promise<unknown>) => {
    await p
    onDone()
  }
  const daysN = Number(days)
  const daysOK = Number.isInteger(daysN) && daysN >= 1 && daysN <= 3650
  const bytes = parseGB(gb)
  const pickedPlan = plans.find((p) => String(p.id) === planId)
  const amountN = Number(amount)
  const amountOK = amount !== "" && Number.isInteger(amountN) && amountN >= 0
  const free = assignable(addresses, card.addresses)
  const pickedAddress = free.find((a) => String(a.id) === addressId)

  return (
    <>
      <ActionDialog
        open={open("extend")}
        onOpenChange={close}
        title="Продлить подписку"
        description={current ? `Сейчас: ${current.plan_name}, до ${fmtDate(current.expires_at)}` : undefined}
        consequences={
          <>
            <p>Окончание текущего периода сдвинется на указанное число дней, периоды в очереди — вслед за ним.</p>
            {current?.traffic_exhausted && (
              <p className="text-warning">Трафик периода исчерпан: продление не восстановит доступ. Сначала добавьте трафик.</p>
            )}
          </>
        }
        confirmLabel="Продлить"
        canConfirm={daysOK}
        success="Подписка продлена"
        onConfirm={() => run(core.post(`/v1/admin/users/${uid}/extend`, { days: daysN }))}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="days">На сколько дней</Label>
          <Input id="days" inputMode="numeric" value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))} placeholder="7" autoFocus />
          <Chips values={[["1", "1 день"], ["3", "3 дня"], ["7", "неделя"], ["30", "месяц"]].map(([value, label]) => ({ label, value }))} onPick={setDays} />
        </div>
      </ActionDialog>

      <ActionDialog
        open={open("traffic")}
        onOpenChange={close}
        title="Добавить трафик"
        description={current ? `Период: ${current.plan_name}, ${poolUsage(current, pool)}` : undefined}
        consequences={<p>Бонус увеличит лимит выбранного пула в текущем периоде; тариф не изменится. Если пул был исчерпан, доступ к его входам восстановится при следующей синхронизации нод.</p>}
        confirmLabel={bytes ? `Добавить ${fmtBytes(bytes)}` : "Добавить"}
        canConfirm={bytes !== null && !!current && poolLimited(current, pool)}
        success="Трафик добавлен"
        onConfirm={() => run(core.post(`/v1/admin/users/${uid}/traffic`, { bytes, pool_id: Number(pool) }))}
      >
        {current && current.pools.length > 0 && (
          <div className="flex flex-col gap-2">
            <Label>Пул</Label>
            <Select value={pool} onValueChange={setPool}>
              <SelectTrigger aria-label="Пул">
                <SelectValue placeholder="Выберите пул" />
              </SelectTrigger>
              <SelectContent>
                {current.pools.map((x) => (
                  <SelectItem key={x.pool_id} value={String(x.pool_id)} disabled={x.traffic_limit_bytes === null}>
                    {x.name}
                    {x.traffic_limit_bytes === null ? " — без лимита" : x.exhausted ? " — исчерпан" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <div className="flex flex-col gap-2">
          <Label htmlFor="gb">Объём, ГБ</Label>
          <Input id="gb" inputMode="decimal" value={gb} onChange={(e) => setGb(e.target.value)} placeholder="10" autoFocus />
          <Chips values={["5", "10", "50", "100"].map((v) => ({ label: `${v} ГБ`, value: v }))} onPick={setGb} />
        </div>
      </ActionDialog>

      <ActionDialog
        open={open("plan")}
        onOpenChange={close}
        title="Сменить тариф без оплаты"
        description={current ? `Сейчас: ${current.plan_name}` : undefined}
        consequences={<p>Текущий период перейдёт на другой тариф: даты сохранятся, очередь не изменится. Доступные ноды и лимиты определяются новым тарифом; расход пулов, которых нет в новом тарифе, не переносится.</p>}
        confirmLabel="Сменить тариф"
        canConfirm={!!pickedPlan}
        success="Тариф сменён"
        onConfirm={() => run(core.post(`/v1/admin/users/${uid}/plan`, { plan_id: Number(planId) }))}
      >
        <div className="flex flex-col gap-2">
          <Label>Новый тариф</Label>
          <Select value={planId} onValueChange={setPlanId}>
            <SelectTrigger>
              <SelectValue placeholder="Выберите тариф" />
            </SelectTrigger>
            <SelectContent>
              {plans
                .filter((p) => p.id !== current?.plan_id)
                .map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name} · {p.period_days} дн · {fmtPlanTraffic(p)}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </div>
      </ActionDialog>

      <ActionDialog
        open={open("manual")}
        onOpenChange={close}
        title="Выдать вручную"
        description="Регистрация оплаты, полученной не через ЮKassa"
        consequences={<p>Как при обычной оплате: тот же тариф встанет в очередь после текущего периода, другой тариф начнёт действовать сразу, а остаток текущего периода будет аннулирован.</p>}
        confirmLabel={pickedPlan ? `Выдать «${pickedPlan.name}»` : "Выдать"}
        canConfirm={!!pickedPlan && amountOK}
        success="Период выдан"
        onConfirm={() => run(core.post("/v1/payments", { payment_id: paymentId, user_id: uid, plan_id: Number(planId), amount_rub: amountN }))}
      >
        <div className="flex flex-col gap-2">
          <Label>Тариф</Label>
          <Select
            value={planId}
            onValueChange={(v) => {
              setPlanId(v)
              const p = plans.find((x) => String(x.id) === v)
              if (p) setAmount(String(p.price_rub))
            }}
          >
            <SelectTrigger>
              <SelectValue placeholder="Выберите тариф" />
            </SelectTrigger>
            <SelectContent>
              {plans.map((p) => (
                <SelectItem key={p.id} value={String(p.id)}>
                  {p.name} · {fmtRub(p.price_rub)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="amount">Сумма, ₽</Label>
          <Input id="amount" inputMode="numeric" value={amount} onChange={(e) => setAmount(e.target.value.replace(/\D/g, ""))} placeholder="0" />
          <p className="text-xs text-muted-foreground">Сумма учитывается в выручке как ручная оплата; для подарка укажите 0 ₽. Номер платежа: {paymentId}</p>
        </div>
      </ActionDialog>

      <ActionDialog
        open={open("rotate")}
        onOpenChange={close}
        title="Сменить ссылку подписки"
        consequences={
          <>
            <p>Текущая ссылка сразу перестанет работать, пользователю потребуется добавить новую ссылку в клиент.</p>
            <p>Прежний ключ будет отозван; уже установленные соединения сохранятся до разрыва.</p>
          </>
        }
        destructive
        confirmLabel="Сменить ссылку"
        success="Ссылка сменена"
        onConfirm={() => run(core.post(`/v1/users/${uid}/rotate`))}
      />

      <ActionDialog
        open={open("ban")}
        onOpenChange={close}
        title="Заблокировать пользователя"
        consequences={
          <>
            <p>Текущий период и все периоды в очереди будут закрыты, доступы к приватным тарифам — отозваны.</p>
            <p>Ссылка подписки перестанет работать, бот перестанет отвечать пользователю, ноды отключат его при следующей синхронизации.</p>
            <p className="font-medium">Разблокировка не восстанавливает периоды; их можно выдать только вручную.</p>
          </>
        }
        destructive
        reason={{ label: "Причина", placeholder: "например, нарушение условий использования" }}
        confirmLabel="Заблокировать"
        success="Пользователь заблокирован"
        onConfirm={(reason) => run(core.post(`/v1/admin/users/${uid}/ban`, reason ? { reason } : {}))}
      />

      <ActionDialog
        open={open("unban")}
        onOpenChange={close}
        title="Разблокировать пользователя"
        consequences={<p>Бот и ссылка подписки снова заработают, но подписки не будет: её нужно купить или выдать вручную. Доступы к приватным тарифам и пробный период не восстанавливаются.</p>}
        confirmLabel="Разблокировать"
        success="Пользователь разблокирован"
        onConfirm={() => run(core.post(`/v1/admin/users/${uid}/unban`))}
      />

      <ActionDialog
        open={open("close")}
        onOpenChange={close}
        title="Закрыть период"
        description={dialog?.kind === "close" ? `${dialog.period.plan_name}: ${fmtDate(dialog.period.starts_at)} — ${fmtDate(dialog.period.expires_at)}` : undefined}
        consequences={
          dialog?.kind === "close" && dialog.period.state === "current" ? (
            <p>Период завершится сейчас, следующий период из очереди начнётся сразу. Если очереди нет, пользователь останется без подписки.</p>
          ) : (
            <p>Период будет удалён из очереди, следующие периоды сдвинутся на его длительность.</p>
          )
        }
        destructive
        confirmLabel="Закрыть период"
        success="Период закрыт"
        onConfirm={() => run(dialog?.kind === "close" ? core.post(`/v1/admin/periods/${dialog.period.id}/close`) : Promise.resolve())}
      />

      <ActionDialog
        open={open("resetDevices")}
        onOpenChange={close}
        title="Отвязать все устройства"
        consequences={<p>Пользователь сможет снова подключить устройства в пределах лимита тарифа.</p>}
        destructive
        confirmLabel="Отвязать все"
        success="Устройства отвязаны"
        onConfirm={() => run(core.post(`/v1/users/${uid}/devices/reset`))}
      />

      <ActionDialog
        open={open("assignAddress")}
        onOpenChange={close}
        title="Закрепить выделенный IP"
        consequences={
          <>
            <p>На ноде этого адреса трафик пользователя пойдёт в интернет с этого IP, если у его тарифа включён выделенный IP. На других нодах ничего не изменится.</p>
            <p>Нода перезапустит xray: открытые соединения на ней прервутся на несколько секунд.</p>
          </>
        }
        confirmLabel={pickedAddress ? `Закрепить ${pickedAddress.ip}` : "Закрепить"}
        canConfirm={!!pickedAddress && note.length <= 200}
        success="Адрес закреплён"
        onConfirm={() => run(core.put(`/v1/admin/addresses/${addressId}/assignment`, { user_id: uid, note: note.trim() }))}
      >
        <div className="flex flex-col gap-2">
          <Label>Адрес</Label>
          <Select value={addressId} onValueChange={setAddressId}>
            <SelectTrigger>
              <SelectValue placeholder={free.length === 0 ? "свободных адресов нет" : "Выберите адрес"} />
            </SelectTrigger>
            <SelectContent>
              {free.map((a) => (
                <SelectItem key={a.id} value={String(a.id)}>
                  {a.ip} · {a.node_name}
                  {a.status === "error" ? " · с ошибкой" : ""}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">Показаны свободные адреса на нодах, где у пользователя ещё нет закреплённого адреса.</p>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="anote">Заметка</Label>
          <Input id="anote" value={note} onChange={(e) => setNote(e.target.value)} placeholder="например, срок аренды адреса" />
        </div>
      </ActionDialog>

      <ActionDialog
        open={open("unassignAddress")}
        onOpenChange={close}
        title="Снять выделенный IP"
        description={dialog?.kind === "unassignAddress" ? `${dialog.address.ip} · ${dialog.address.node_name}` : undefined}
        consequences={
          <>
            <p>Адрес освободится, трафик пользователя пойдёт через общий адрес ноды.</p>
            <p>Нода перезапустит xray: все открытые соединения на ней прервутся на несколько секунд.</p>
          </>
        }
        destructive
        confirmLabel="Снять"
        success="Адрес снят"
        onConfirm={() => run(dialog?.kind === "unassignAddress" ? core.del(`/v1/admin/addresses/${dialog.address.address_id}/assignment`) : Promise.resolve())}
      />

      <ActionDialog
        open={open("deleteDevice")}
        onOpenChange={close}
        title="Отвязать устройство"
        description={dialog?.kind === "deleteDevice" ? dialog.name : undefined}
        consequences={<p>Место освободится; устройство сможет снова привязаться при следующем обновлении подписки.</p>}
        confirmLabel="Отвязать"
        success="Устройство отвязано"
        onConfirm={() => run(dialog?.kind === "deleteDevice" ? core.del(`/v1/users/${uid}/devices/${dialog.deviceId}`) : Promise.resolve())}
      />
    </>
  )
}

function poolLimited(p: Period, pool: string): boolean {
  return p.pools.some((x) => String(x.pool_id) === pool && x.traffic_limit_bytes !== null)
}

function poolUsage(p: Period, pool: string): string {
  const x = p.pools.find((y) => String(y.pool_id) === pool)
  return x ? `пул «${x.name}»: использовано ${fmtBytes(x.used_bytes)} из ${fmtBytes(x.traffic_limit_bytes)}` : "пул не выбран"
}
