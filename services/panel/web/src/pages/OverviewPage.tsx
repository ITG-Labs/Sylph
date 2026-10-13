import { useQuery } from "@tanstack/react-query"
import { Link } from "react-router"
import { AlertTriangle, ArrowRight, Ban, CircleCheck, Clock, CreditCard, Server, ShieldAlert, Users, Wallet } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { NodeStatusBadge } from "@/components/common/NodeStatusBadge"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { StatTile } from "@/components/common/StatTile"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { core } from "@/lib/api"
import { fmtBytes, fmtRub, plural } from "@/lib/format"
import type { Overview } from "@/lib/types"

function AttentionItem({ to, icon, count, label }: { to: string; icon: React.ReactNode; count: number; label: string }) {
  return (
    <Link
      to={to}
      className="group flex items-center gap-3 rounded-xl border border-warning/25 bg-warning/[0.06] px-4 py-3 transition-colors hover:bg-warning/10"
    >
      <span className="text-warning [&_svg]:size-5">{icon}</span>
      <span className="text-2xl font-semibold tabular-nums">{count}</span>
      <span className="text-sm text-muted-foreground group-hover:text-foreground">{label}</span>
      <ArrowRight className="ml-auto size-4 text-muted-foreground opacity-0 transition group-hover:opacity-100" />
    </Link>
  )
}

export function OverviewPage() {
  const q = useQuery({ queryKey: ["overview"], queryFn: () => core.get<Overview>("/v1/admin/overview"), refetchInterval: 30_000 })

  if (q.isPending) {
    return (
      <>
        <PageHeader title="Сводка" />
        <LoadingBlock rows={4} />
      </>
    )
  }
  if (q.isError) {
    return (
      <>
        <PageHeader title="Сводка" />
        <LoadError error={q.error} onRetry={() => q.refetch()} />
      </>
    )
  }

  const o = q.data
  const a = o.attention
  const attention = [
    { n: a.nodes_problem, to: "/nodes", icon: <Server />, label: plural(a.nodes_problem, "нода лежит или с проблемой", "ноды лежат или с проблемой", "нод лежат или с проблемой") },
    { n: a.mismatch, to: "/payments?status=mismatch", icon: <AlertTriangle />, label: plural(a.mismatch, "платёж с расхождением суммы", "платежа с расхождением суммы", "платежей с расхождением суммы") },
    { n: a.stuck_pending, to: "/payments?stuck=true", icon: <Clock />, label: plural(a.stuck_pending, "платёж висит дольше суток", "платежа висят дольше суток", "платежей висят дольше суток") },
    { n: a.paid_while_banned, to: "/users?status=banned", icon: <Ban />, label: plural(a.paid_while_banned, "оплата от забаненного", "оплаты от забаненных", "оплат от забаненных") },
  ].filter((x) => x.n > 0)
  const rev30 = o.revenue.yookassa.days_30 + o.revenue.manual.days_30
  const revToday = o.revenue.yookassa.today + o.revenue.manual.today
  const online = o.nodes.by_status.online ?? 0

  return (
    <>
      <PageHeader
        title="Сводка"
        description={
          <span>
            обновлено {new Date(q.dataUpdatedAt).toLocaleTimeString("ru-RU")} · каждые 30 с
          </span>
        }
      />

      {attention.length > 0 ? (
        <section className="mb-6">
          <h2 className="mb-3 flex items-center gap-2 text-sm font-medium text-warning">
            <ShieldAlert className="size-4" />
            Требует внимания
          </h2>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {attention.map((x) => (
              <AttentionItem key={x.to} to={x.to} icon={x.icon} count={x.n} label={x.label} />
            ))}
          </div>
        </section>
      ) : (
        <div className="mb-6 flex items-center gap-2 rounded-xl border border-success/20 bg-success/[0.06] px-4 py-3 text-sm">
          <CircleCheck className="size-4 text-success" />
          Всё спокойно — ничего не требует внимания
        </div>
      )}

      <div className="mb-6 grid grid-cols-2 gap-3 xl:grid-cols-4">
        <StatTile label="Юзеры" icon={<Users />} value={o.users.total} hint={`+${o.users.new_today} сегодня · +${o.users.new_7d} за 7 дней`} />
        <StatTile
          label="Активные подписки"
          icon={<CircleCheck />}
          value={o.users.active_subscription}
          hint={`из них пробных ${o.users.trial} · забанено ${o.users.banned}`}
        />
        <StatTile label="Выручка за 30 дней" icon={<Wallet />} value={fmtRub(rev30)} hint={`сегодня ${fmtRub(revToday)}`} />
        <StatTile
          label="Ноды онлайн"
          icon={<Server />}
          value={
            <span>
              {online}
              <span className="text-lg text-muted-foreground"> / {o.nodes.total}</span>
            </span>
          }
          hint={`юзеров на нодах ${o.nodes.active_users}`}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-5">
        <Card className="xl:col-span-3">
          <CardHeader>
            <CardTitle>Ноды</CardTitle>
            <CardDescription>юзеры сейчас и трафик за сегодня</CardDescription>
            <CardAction>
              <Button variant="ghost" size="sm" asChild>
                <Link to="/nodes">
                  все ноды <ArrowRight />
                </Link>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col divide-y">
            {o.nodes.nodes.length === 0 && <div className="py-6 text-sm text-muted-foreground">Нод пока нет</div>}
            {o.nodes.nodes.map((n) => (
              <Link
                key={n.id}
                to={`/nodes/${n.id}`}
                className="-mx-2 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg px-2 py-3 transition-colors hover:bg-white/[0.03]"
              >
                <div className="min-w-0 basis-full sm:basis-0 sm:flex-1">
                  <div className="truncate font-medium">{n.name}</div>
                  <div className="mt-1">
                    <NodeStatusBadge status={n.status} />
                  </div>
                </div>
                <div className="text-left sm:text-right">
                  <div className="font-medium tabular-nums">{n.active_users ?? "—"}</div>
                  <div className="text-xs text-muted-foreground">юзеров</div>
                </div>
                <div className="w-24 text-left sm:text-right">
                  <div className="font-medium tabular-nums">{fmtBytes(n.traffic_today)}</div>
                  <div className="text-xs text-muted-foreground">сегодня</div>
                </div>
              </Link>
            ))}
          </CardContent>
        </Card>

        <Card className="xl:col-span-2">
          <CardHeader>
            <CardTitle>Выручка</CardTitle>
            <CardDescription>по суткам Москвы</CardDescription>
            <CardAction>
              <Button variant="ghost" size="sm" asChild>
                <Link to="/payments">
                  <CreditCard /> платежи
                </Link>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-xs text-muted-foreground">
                  <th className="pb-2 text-left font-normal" />
                  <th className="pb-2 text-right font-normal">сегодня</th>
                  <th className="pb-2 text-right font-normal">7 дней</th>
                  <th className="pb-2 text-right font-normal">30 дней</th>
                </tr>
              </thead>
              <tbody className="tabular-nums">
                <tr className="border-t">
                  <td className="py-2 text-muted-foreground">ЮKassa</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.yookassa.today)}</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.yookassa.days_7)}</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.yookassa.days_30)}</td>
                </tr>
                <tr className="border-t">
                  <td className="py-2 text-muted-foreground">Вручную</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.manual.today)}</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.manual.days_7)}</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.manual.days_30)}</td>
                </tr>
                <tr className="border-t font-medium">
                  <td className="py-2">Всего</td>
                  <td className="py-2 text-right">{fmtRub(revToday)}</td>
                  <td className="py-2 text-right">{fmtRub(o.revenue.yookassa.days_7 + o.revenue.manual.days_7)}</td>
                  <td className="py-2 text-right">{fmtRub(rev30)}</td>
                </tr>
              </tbody>
            </table>
            <div>
              <div className="mb-2 text-xs text-muted-foreground">по тарифам за 30 дней</div>
              {o.revenue.by_plan_30d.length === 0 ? (
                <div className="text-sm text-muted-foreground">оплат через ЮKassa не было</div>
              ) : (
                <ul className="flex flex-col gap-1.5 text-sm">
                  {o.revenue.by_plan_30d.map((p) => (
                    <li key={p.plan_id} className="flex items-center justify-between gap-3">
                      <span className="truncate">{p.name}</span>
                      <span className="text-muted-foreground tabular-nums">
                        {p.count} шт · <span className="text-foreground">{fmtRub(p.amount_rub)}</span>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            {o.revenue.refunded_30d > 0 && (
              <div className="rounded-lg bg-white/[0.03] px-3 py-2 text-sm text-muted-foreground">
                Возвращено за 30 дней: <span className="text-foreground">{fmtRub(o.revenue.refunded_30d)}</span>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  )
}
