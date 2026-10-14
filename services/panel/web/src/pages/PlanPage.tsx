import { useState } from "react"
import { Link, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, Link2, Pencil, Plus, UserPlus, X } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { CopyButton } from "@/components/common/CopyButton"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { PlanBadges } from "@/components/plan/PlanBadges"
import { PlanFormDialog } from "@/components/plan/PlanFormDialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { core } from "@/lib/api"
import { fmtBytes, fmtDate, fmtDateTime, fmtRub } from "@/lib/format"
import { useGroups, useInvalidate, usePlans } from "@/lib/queries"
import type { Access, Invite } from "@/lib/types"

type Dialog = { kind: "edit" } | { kind: "invite" } | { kind: "revokeInvite"; invite: Invite } | { kind: "grant" } | { kind: "revokeAccess"; access: Access }

function Fact({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="well rounded-xl p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-medium tabular-nums">{value}</div>
    </div>
  )
}

export function PlanPage() {
  const { id } = useParams()
  const plans = usePlans()
  const groups = useGroups()
  const invalidate = useInvalidate()
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [maxUses, setMaxUses] = useState("")
  const [expires, setExpires] = useState("")
  const [tg, setTg] = useState("")
  const plan = plans.data?.find((p) => String(p.id) === id)
  const isPrivate = plan?.visibility === "private"
  const invites = useQuery({ queryKey: ["invites", id], queryFn: () => core.get<Invite[]>(`/v1/admin/plans/${id}/invites`), enabled: isPrivate })
  const access = useQuery({ queryKey: ["access", id], queryFn: () => core.get<Access[]>(`/v1/admin/plans/${id}/access`), enabled: isPrivate })

  if (plans.isPending) return <LoadingBlock rows={3} />
  if (plans.isError) return <LoadError error={plans.error} onRetry={() => plans.refetch()} />
  if (!plan) return <Empty>Тариф не найден</Empty>
  const close = (v: boolean) => !v && setDialog(null)
  const open = (d: Dialog) => {
    setMaxUses("")
    setExpires("")
    setTg("")
    setDialog(d)
  }
  const group = plan.location_group_id === null ? "только общие ноды" : `${groups.data?.find((g) => g.id === plan.location_group_id)?.name ?? `группа ${plan.location_group_id}`} + общие`
  const maxUsesN = maxUses === "" ? null : Number(maxUses)
  const expiresISO = expires ? new Date(expires).toISOString() : null

  return (
    <>
      <Link to="/plans" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Тарифы
      </Link>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            {plan.name}
            <PlanBadges plan={plan} />
          </span>
        }
        description={`создан ${fmtDate(plan.created_at)}`}
        actions={
          <Button variant="secondary" onClick={() => open({ kind: "edit" })}>
            <Pencil />
            Изменить
          </Button>
        }
      />
      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-5">
        <Fact label="Цена" value={fmtRub(plan.price_rub)} />
        <Fact label="Период" value={`${plan.period_days} дн`} />
        <Fact label="Трафик" value={fmtBytes(plan.traffic_limit_bytes)} />
        <Fact label="Устройств" value={`${plan.device_limit ?? "∞"}${plan.hwid_required ? " · HWID" : ""}`} />
        <Fact label="Ноды" value={group} />
      </div>

      {isPrivate ? (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <Section
            title="Приглашения"
            description="ссылка на бота: юзер жмёт /start и получает доступ"
            action={
              <Button size="sm" onClick={() => open({ kind: "invite" })}>
                <Plus />
                Создать
              </Button>
            }
          >
            {invites.isPending ? (
              <LoadingBlock rows={1} />
            ) : invites.isError ? (
              <LoadError error={invites.error} />
            ) : invites.data.length === 0 ? (
              <Empty>Приглашений нет</Empty>
            ) : (
              <ul className="flex flex-col gap-2">
                {[...invites.data].sort((a, b) => b.id - a.id).map((inv) => {
                  const dead = !!inv.revoked_at || (inv.expires_at !== null && new Date(inv.expires_at) < new Date()) || (inv.max_uses !== null && inv.uses >= inv.max_uses)
                  return (
                    <li key={inv.id} className="flex flex-col gap-2 well rounded-xl p-3">
                      <div className="flex items-center gap-2">
                        <Link2 className="size-4 shrink-0 text-muted-foreground" />
                        <code className="min-w-0 flex-1 truncate font-mono text-xs">{inv.link ?? inv.code}</code>
                        <CopyButton value={inv.link ?? inv.code} label="Скопировать ссылку" />
                        {!inv.revoked_at && (
                          <Button variant="ghost" size="icon-sm" aria-label="Отозвать приглашение" onClick={() => open({ kind: "revokeInvite", invite: inv })}>
                            <X />
                          </Button>
                        )}
                      </div>
                      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                        <span className="tabular-nums">
                          использовано {inv.uses} из {inv.max_uses ?? "∞"}
                        </span>
                        <span>· {inv.expires_at ? `до ${fmtDateTime(inv.expires_at)}` : "бессрочно"}</span>
                        {inv.revoked_at ? <Badge variant="destructive">отозвано</Badge> : dead ? <Badge variant="outline">не действует</Badge> : <Badge variant="success">действует</Badge>}
                      </div>
                    </li>
                  )
                })}
              </ul>
            )}
          </Section>

          <Section
            title="Доступы"
            description="кому открыт тариф: по приглашению или вручную"
            action={
              <Button size="sm" onClick={() => open({ kind: "grant" })}>
                <UserPlus />
                Открыть
              </Button>
            }
          >
            {access.isPending ? (
              <LoadingBlock rows={1} />
            ) : access.isError ? (
              <LoadError error={access.error} />
            ) : access.data.length === 0 ? (
              <Empty>Тариф пока никому не открыт</Empty>
            ) : (
              <ul className="flex flex-col divide-y">
                {access.data.map((a) => (
                  <li key={a.user_id} className="flex items-center gap-3 py-2.5 text-sm">
                    <div className="min-w-0 flex-1">
                      <Link to={`/users/${a.user_id}`} className="font-medium hover:underline">
                        {a.telegram_id ?? `№ ${a.user_id}`}
                      </Link>
                      <div className="text-xs text-muted-foreground">
                        {a.source === "invite" ? "по приглашению" : "вручную"} · {fmtDate(a.granted_at)}
                      </div>
                    </div>
                    {a.revoked_at ? (
                      <Badge variant="outline">отозван {fmtDate(a.revoked_at)}</Badge>
                    ) : a.telegram_id === null ? (
                      <span className="text-xs text-muted-foreground">нет Telegram ID — отозвать нельзя</span>
                    ) : (
                      <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => open({ kind: "revokeAccess", access: a })}>
                        Отозвать
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </Section>
        </div>
      ) : (
        <Empty>Тариф публичный — его видят все в меню бота{plan.on_sale ? "" : ", но сейчас он снят с продажи"}. Приглашения и доступы — у приватных.</Empty>
      )}

      <PlanFormDialog open={dialog?.kind === "edit"} onOpenChange={close} plan={plan} groups={groups.data ?? []} onDone={() => invalidate(["plans"], ["overview"])} />
      <ActionDialog
        open={dialog?.kind === "invite"}
        onOpenChange={close}
        title="Новое приглашение"
        description={`в тариф «${plan.name}»`}
        confirmLabel="Создать"
        canConfirm={maxUsesN === null || (Number.isInteger(maxUsesN) && maxUsesN >= 1 && maxUsesN <= 100000)}
        success="Приглашение создано — ссылку можно скопировать"
        onConfirm={async () => {
          await core.post(`/v1/admin/plans/${plan.id}/invites`, { max_uses: maxUsesN, expires_at: expiresISO })
          await invalidate(["invites", id])
        }}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-2">
            <Label htmlFor="max-uses">Сколько раз</Label>
            <Input id="max-uses" inputMode="numeric" value={maxUses} onChange={(e) => setMaxUses(e.target.value.replace(/\D/g, ""))} placeholder="пусто — без лимита" />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="expires">Действует до</Label>
            <Input id="expires" type="datetime-local" value={expires} onChange={(e) => setExpires(e.target.value)} />
          </div>
        </div>
      </ActionDialog>
      <ActionDialog
        open={dialog?.kind === "revokeInvite"}
        onOpenChange={close}
        title="Отозвать приглашение"
        consequences={<p>Ссылка перестанет работать. Доступы, уже выданные по ней, останутся — их отзывают отдельно.</p>}
        destructive
        confirmLabel="Отозвать"
        success="Приглашение отозвано"
        onConfirm={async () => {
          if (dialog?.kind !== "revokeInvite") return
          await core.del(`/v1/admin/invites/${dialog.invite.id}`)
          await invalidate(["invites", id])
        }}
      />
      <ActionDialog
        open={dialog?.kind === "grant"}
        onOpenChange={close}
        title="Открыть тариф по Telegram ID"
        description="Если юзер ещё не нажимал /start, он заведётся сам"
        confirmLabel="Открыть"
        canConfirm={/^\d{1,19}$/.test(tg)}
        success="Доступ открыт"
        onConfirm={async () => {
          await core.put(`/v1/admin/plans/${plan.id}/access/${tg}`)
          await invalidate(["access", id], ["users"])
        }}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="tg">Telegram ID</Label>
          <Input id="tg" inputMode="numeric" value={tg} onChange={(e) => setTg(e.target.value.replace(/\D/g, ""))} placeholder="123456789" autoFocus />
        </div>
      </ActionDialog>
      <ActionDialog
        open={dialog?.kind === "revokeAccess"}
        onOpenChange={close}
        title="Отозвать доступ"
        description={dialog?.kind === "revokeAccess" ? `Юзер ${dialog.access.telegram_id ?? dialog.access.user_id}` : undefined}
        consequences={<p>Юзер больше не увидит тариф в меню и не купит его заново, повторное приглашение не поможет. Текущий оплаченный период доживёт, продлить его можно.</p>}
        destructive
        confirmLabel="Отозвать"
        success="Доступ отозван"
        onConfirm={async () => {
          if (dialog?.kind !== "revokeAccess") return
          await core.del(`/v1/admin/plans/${plan.id}/access/${dialog.access.telegram_id}`)
          await invalidate(["access", id])
        }}
      />
    </>
  )
}
