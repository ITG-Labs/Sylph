import { useState } from "react"
import { Link, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, Ban, CalendarPlus, ChevronRight, Gift, Gauge, History, KeyRound, MoreHorizontal, Repeat, ShieldCheck, Smartphone, Trash2 } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { CopyButton } from "@/components/common/CopyButton"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { PaymentStatusBadge, SourceLabel, UserStatusBadge } from "@/components/common/StatusBadges"
import { TrafficBar } from "@/components/common/TrafficBar"
import { UserDialogs, type UserDialog } from "@/components/user/UserDialogs"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { core, qs } from "@/lib/api"
import { fmtBytes, fmtDate, fmtDateTime, fmtRelative, fmtRub } from "@/lib/format"
import { useInvalidate, usePlans } from "@/lib/queries"
import type { AuditPage, Period, UserCard, UserStats } from "@/lib/types"

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-1.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right tabular-nums">{children}</span>
    </div>
  )
}

const auditAction: Record<string, string> = {
  ban: "забанил",
  unban: "разбанил",
  extend: "продлил",
  traffic: "докинул трафик",
  plan: "сменил тариф",
  rotate: "сменил ссылку",
  reset: "отвязал все устройства",
  devices: "отвязал устройство",
}

function describeAudit(action: string, details: Record<string, unknown> | null) {
  const base = auditAction[action] ?? action
  if (!details) return base
  if (typeof details.days === "number") return `${base} на ${details.days} дн`
  if (typeof details.bytes === "number") return `${base}: +${fmtBytes(details.bytes)}`
  if (typeof details.reason === "string" && details.reason) return `${base}: «${details.reason}»`
  return base
}

export function UserPage() {
  const { id } = useParams()
  const invalidate = useInvalidate()
  const plans = usePlans()
  const [dialog, setDialog] = useState<UserDialog | null>(null)
  const card = useQuery({ queryKey: ["user", id], queryFn: () => core.get<UserCard>(`/v1/admin/users/${id}`) })
  const banned = card.data?.user.status === "banned"
  const stats = useQuery({
    queryKey: ["user-stats", id, card.data?.user.status],
    queryFn: () => core.get<UserStats>(`/v1/users/${id}/stats`),
    enabled: !!card.data && !banned,
  })
  const history = useQuery({
    queryKey: ["audit", "user", id],
    queryFn: async () => {
      const [a, b] = await Promise.all(
        ["user", "users"].map((entity) => core.get<AuditPage>(`/v1/admin/audit${qs({ entity, entity_id: id, limit: 15 })}`)),
      )
      return [...a.entries, ...b.entries].sort((x, y) => y.id - x.id).slice(0, 15)
    },
  })

  if (card.isPending) return <LoadingBlock rows={5} />
  if (card.isError) return <LoadError error={card.error} onRetry={() => card.refetch()} />

  const c = card.data
  const u = c.user
  const current = c.periods.find((p) => p.state === "current")
  const queued = c.periods.filter((p) => p.state === "queued")
  const closed = c.periods.filter((p) => p.state !== "current" && p.state !== "queued")
  const paidUntil = queued.length > 0 ? queued[queued.length - 1].expires_at : current?.expires_at
  const open = (d: UserDialog) => () => setDialog(d)
  const refresh = () => invalidate(["user", id], ["users"], ["overview"], ["audit"], ["payments"])

  return (
    <>
      <Link to="/users" className="mb-4 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4" />
        Юзеры
      </Link>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            {u.telegram_id ?? `Юзер № ${u.id}`}
            <UserStatusBadge status={u.status} />
          </span>
        }
        description={`№ ${u.id} · с нами с ${fmtDate(u.created_at)}`}
        actions={
          banned ? (
            <Button onClick={open({ kind: "unban" })}>
              <ShieldCheck />
              Разбанить
            </Button>
          ) : (
            <div className="grid w-full grid-cols-[1fr_1fr_1fr_auto] gap-2 sm:flex sm:w-auto">
              <Button variant="secondary" className="max-sm:h-8 max-sm:px-2 max-sm:[&_svg]:hidden" onClick={open({ kind: "extend" })} disabled={!current}>
                <CalendarPlus />
                Продлить
              </Button>
              <Button variant="secondary" className="max-sm:h-8 max-sm:px-2 max-sm:[&_svg]:hidden" onClick={open({ kind: "traffic" })} disabled={!current || current.traffic_limit_bytes === null}>
                <Gauge />
                Трафик
              </Button>
              <Button variant="secondary" className="max-sm:h-8 max-sm:px-2 max-sm:[&_svg]:hidden" onClick={open({ kind: "manual" })}>
                <Gift />
                Выдать
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="icon" className="max-sm:size-8" aria-label="Ещё действия">
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem disabled={!current} onSelect={open({ kind: "plan" })}>
                    <Repeat />
                    Сменить тариф без оплаты
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={open({ kind: "rotate" })}>
                    <KeyRound />
                    Сменить ссылку подписки
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onSelect={open({ kind: "ban" })}>
                    <Ban />
                    Забанить
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )
        }
      />

      {banned && (
        <div className="mb-6 flex gap-3 rounded-xl border border-destructive/30 bg-destructive/[0.07] p-4 text-sm">
          <Ban className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div>
            <div className="font-medium">Забанен {fmtDateTime(u.banned_at)}</div>
            <div className="text-muted-foreground">{u.ban_reason ? `Причина: ${u.ban_reason}` : "Причина не указана"}</div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <div className="flex flex-col gap-4 xl:col-span-2">
          <Section
            title="Подписка"
            description={current ? `оплачено до ${fmtDate(paidUntil)}` : "активного периода нет"}
            action={
              current && (
                <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={open({ kind: "close", period: current })}>
                  Закрыть период
                </Button>
              )
            }
          >
            {current ? (
              <div className="flex flex-col gap-4">
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span className="text-xl font-semibold">{current.plan_name}</span>
                  <span className="text-sm text-muted-foreground tabular-nums">
                    {fmtDate(current.starts_at)} — {fmtDate(current.expires_at)}
                  </span>
                  {c.trial?.period_id === current.id && <Badge variant="info">пробный</Badge>}
                </div>
                <TrafficBar used={current.traffic_used_bytes} limit={current.traffic_limit_bytes} />
                {current.traffic_bonus_bytes > 0 && <div className="text-xs text-muted-foreground">в лимите бонус {fmtBytes(current.traffic_bonus_bytes)}</div>}
                {queued.length > 0 && (
                  <div>
                    <div className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">В очереди</div>
                    <ul className="flex flex-col divide-y rounded-lg border">
                      {queued.map((p) => (
                        <PeriodLine key={p.id} p={p} onClose={open({ kind: "close", period: p })} />
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            ) : (
              <Empty>Подписки нет — можно выдать вручную</Empty>
            )}
          </Section>

          <Section title="Трафик по серверам" description="вверх + вниз, сутки по Москве">
            {banned ? (
              <Empty>У забаненного статистика не показывается</Empty>
            ) : stats.isPending ? (
              <LoadingBlock rows={1} />
            ) : stats.isError ? (
              <LoadError error={stats.error} />
            ) : stats.data.nodes.length === 0 ? (
              <Empty>Трафика ещё не было</Empty>
            ) : (
              <Tabs defaultValue="today">
                <TabsList>
                  <TabsTrigger value="today">Сегодня</TabsTrigger>
                  <TabsTrigger value="days_7">7 дней</TabsTrigger>
                  <TabsTrigger value="days_30">30 дней</TabsTrigger>
                  <TabsTrigger value="period">Период</TabsTrigger>
                </TabsList>
                {(["today", "days_7", "days_30", "period"] as const).map((k) => {
                  const max = Math.max(1, ...stats.data.nodes.map((n) => n[k]))
                  return (
                    <TabsContent key={k} value={k} className="flex flex-col gap-3 pt-1">
                      {stats.data.nodes.map((n) => (
                        <div key={n.node_id} className="flex flex-col gap-1.5">
                          <div className="flex justify-between text-sm">
                            <span>{n.name}</span>
                            <span className="tabular-nums">{fmtBytes(n[k])}</span>
                          </div>
                          <div className="h-1.5 overflow-hidden rounded-full bg-white/8">
                            <div className="h-full rounded-full bg-info/80" style={{ width: `${(n[k] / max) * 100}%` }} />
                          </div>
                        </div>
                      ))}
                    </TabsContent>
                  )
                })}
              </Tabs>
            )}
          </Section>

          <Section
            title="Платежи"
            description="последние 20"
            action={
              c.payments.length > 0 && (
                <Button variant="ghost" size="sm" asChild>
                  <Link to={`/payments?user_id=${u.id}`}>все платежи</Link>
                </Button>
              )
            }
          >
            {c.payments.length === 0 ? (
              <Empty>Платежей не было</Empty>
            ) : (
              <ul className="-mx-2 flex flex-col">
                {c.payments.map((p) => (
                  <li key={p.id}>
                    <Link to={`/payments/${encodeURIComponent(p.payment_id)}`} className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg px-2 py-2.5 hover:bg-white/[0.03]">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">{p.plan_name}</div>
                        <div className="truncate font-mono text-xs text-muted-foreground">{p.payment_id}</div>
                      </div>
                      <PaymentStatusBadge status={p.status} />
                      <div className="w-24 text-right">
                        <div className="text-sm tabular-nums">{fmtRub(p.amount_rub)}</div>
                        <div className="text-xs">
                          <SourceLabel source={p.source} />
                        </div>
                      </div>
                      <div className="hidden w-24 text-right text-xs text-muted-foreground tabular-nums sm:block">{fmtDate(p.paid_at ?? p.created_at)}</div>
                      <ChevronRight className="hidden size-4 text-muted-foreground sm:block" />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Section>

          {closed.length > 0 && (
            <Section title="Прошлые периоды" description="последние 10">
              <ul className="flex flex-col divide-y rounded-lg border">
                {closed.map((p) => (
                  <PeriodLine key={p.id} p={p} />
                ))}
              </ul>
            </Section>
          )}
        </div>

        <div className="flex flex-col gap-4">
          <Section title="Ссылка подписки" description="токен из ссылки — по нему же ищется юзер">
            <div className="flex items-center gap-1 rounded-lg border bg-black/20 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 truncate font-mono text-sm">{u.sub_token}</code>
              <CopyButton value={u.sub_token} label="Скопировать токен" />
            </div>
            {!banned && (
              <Button variant="ghost" size="sm" className="mt-2 -ml-2" onClick={open({ kind: "rotate" })}>
                <KeyRound />
                Сменить ссылку
              </Button>
            )}
          </Section>

          <Section
            title={`Устройства · ${c.devices.length}`}
            action={
              c.devices.length > 0 &&
              !banned && (
                <Button variant="ghost" size="sm" onClick={open({ kind: "resetDevices" })}>
                  Отвязать все
                </Button>
              )
            }
          >
            {c.devices.length === 0 ? (
              <Empty>Устройств нет</Empty>
            ) : (
              <ul className="flex flex-col gap-2">
                {c.devices.map((d) => {
                  const name = [d.model, d.os].filter(Boolean).join(" · ") || d.user_agent || `Устройство ${d.id}`
                  return (
                    <li key={d.id} className="flex items-center gap-3 rounded-lg border bg-white/[0.02] p-3">
                      <Smartphone className="size-4 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm">{name}</div>
                        <div className="truncate text-xs text-muted-foreground">
                          {d.user_agent} · был {fmtRelative(d.last_seen)}
                        </div>
                      </div>
                      {!banned && (
                        <Button variant="ghost" size="icon-sm" aria-label="Отвязать" onClick={open({ kind: "deleteDevice", deviceId: d.id, name })}>
                          <Trash2 />
                        </Button>
                      )}
                    </li>
                  )
                })}
              </ul>
            )}
          </Section>

          <Section title="Пробный и приватные тарифы">
            <div className="flex flex-col gap-1">
              <Row label="Пробный период">{c.trial ? `брал ${fmtDate(c.trial.granted_at)}` : "не брал"}</Row>
              {c.access.length === 0 ? (
                <Row label="Приватные тарифы">нет доступов</Row>
              ) : (
                c.access.map((a) => (
                  <Row key={a.plan_id} label={a.plan_name}>
                    {a.revoked_at ? (
                      <span className="text-muted-foreground">отозван {fmtDate(a.revoked_at)}</span>
                    ) : (
                      <span>
                        открыт {fmtDate(a.granted_at)} · {a.source === "invite" ? "по приглашению" : "админом"}
                      </span>
                    )}
                  </Row>
                ))
              )}
            </div>
          </Section>

          <Section title="История действий" description="бан, продление, трафик, тариф, ссылка и устройства; платежи и периоды — в общем аудите">
            {history.isPending ? (
              <LoadingBlock rows={2} />
            ) : history.isError ? (
              <LoadError error={history.error} />
            ) : history.data.length === 0 ? (
              <Empty>Таких действий с юзером из панели не было</Empty>
            ) : (
              <ol className="flex flex-col gap-3">
                {history.data.map((e) => (
                  <li key={e.id} className="flex gap-3 text-sm">
                    <History className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <div>
                      <div>
                        <span className="font-medium">{e.operator || "без имени"}</span> {describeAudit(e.action, e.details)}
                      </div>
                      <div className="text-xs text-muted-foreground tabular-nums">{fmtDateTime(e.at)}</div>
                    </div>
                  </li>
                ))}
              </ol>
            )}
          </Section>
        </div>
      </div>

      <UserDialogs dialog={dialog} onClose={() => setDialog(null)} card={c} plans={plans.data ?? []} onDone={refresh} />
    </>
  )
}

function PeriodLine({ p, onClose }: { p: Period; onClose?: () => void }) {
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2.5 text-sm">
      <div className="min-w-0 flex-1">
        <div className="font-medium">{p.plan_name}</div>
        <div className="text-xs text-muted-foreground tabular-nums">
          {fmtDate(p.starts_at)} — {fmtDate(p.expires_at)}
          {p.payment_id ? ` · ${p.payment_id}` : ""}
        </div>
      </div>
      <div className="text-xs text-muted-foreground tabular-nums">
        {fmtBytes(p.traffic_used_bytes)} / {fmtBytes(p.traffic_limit_bytes)}
      </div>
      {onClose && (
        <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={onClose}>
          Убрать
        </Button>
      )}
    </li>
  )
}
