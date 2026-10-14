import { useEffect, useState } from "react"
import { Link, useNavigate, useSearchParams } from "react-router"
import { useInfiniteQuery } from "@tanstack/react-query"
import { ChevronRight, Loader2, Search, X } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty } from "@/components/common/Section"
import { UserStatusBadge } from "@/components/common/StatusBadges"
import { TrafficBar } from "@/components/common/TrafficBar"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { core, qs } from "@/lib/api"
import { fmtDate, fmtTelegram } from "@/lib/format"
import { usePlans } from "@/lib/queries"
import type { UsersPage as UsersPageData } from "@/lib/types"

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}

export function UsersPage() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const plans = usePlans()
  const [q, setQ] = useState(params.get("q") ?? "")
  const dq = useDebounced(q.trim(), 350)
  const status = params.get("status") ?? "all"
  const subscription = params.get("subscription") ?? "all"
  const plan = params.get("plan_id") ?? "all"

  useEffect(() => {
    const next = new URLSearchParams(params)
    if (dq) next.set("q", dq)
    else next.delete("q")
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [dq, params, setParams])

  const setFilter = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value === "all") next.delete(key)
    else next.set(key, value)
    setParams(next, { replace: true })
  }

  const filters = {
    q: dq,
    status: status === "all" ? "" : status,
    subscription: subscription === "all" ? "" : subscription,
    plan_id: plan === "all" ? "" : plan,
  }
  const list = useInfiniteQuery({
    queryKey: ["users", filters],
    queryFn: ({ pageParam }) => core.get<UsersPageData>(`/v1/admin/users${qs({ ...filters, before: pageParam, limit: 50 })}`),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
  const users = list.data?.pages.flatMap((p) => p.users) ?? []
  const filtered = !!(filters.q || filters.status || filters.subscription || filters.plan_id)

  return (
    <>
      <PageHeader title="Юзеры" description="поиск по Telegram ID, номеру юзера, @нику, имени или ссылке подписки" />

      <div className="mb-4 flex flex-col gap-2 lg:flex-row">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-10 pr-10 pl-10" placeholder="TG ID, номер, @ник, имя или ссылка" value={q} onChange={(e) => setQ(e.target.value)} />
          {q && (
            <button type="button" className="absolute top-1/2 right-2 flex size-6 -translate-y-1/2 items-center justify-center rounded-full bg-white/10 text-muted-foreground transition hover:bg-white/16 hover:text-foreground" onClick={() => setQ("")} aria-label="Очистить">
              <X className="size-3.5" />
            </button>
          )}
        </div>
        <div className="grid grid-cols-2 gap-2 lg:flex">
          <Select value={status} onValueChange={(v) => setFilter("status", v)}>
            <SelectTrigger className="h-10 lg:w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Все статусы</SelectItem>
              <SelectItem value="active">Активные</SelectItem>
              <SelectItem value="banned">Забаненные</SelectItem>
            </SelectContent>
          </Select>
          <Select value={subscription} onValueChange={(v) => setFilter("subscription", v)}>
            <SelectTrigger className="h-10 lg:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Любая подписка</SelectItem>
              <SelectItem value="active">С подпиской</SelectItem>
              <SelectItem value="none">Без подписки</SelectItem>
            </SelectContent>
          </Select>
          <Select value={plan} onValueChange={(v) => setFilter("plan_id", v)}>
            <SelectTrigger className="col-span-2 h-10 lg:w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Любой тариф</SelectItem>
              {plans.data?.map((p) => (
                <SelectItem key={p.id} value={String(p.id)}>
                  {p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {list.isPending ? (
        <LoadingBlock rows={6} />
      ) : list.isError ? (
        <LoadError error={list.error} onRetry={() => list.refetch()} />
      ) : users.length === 0 ? (
        <Empty>{filtered ? "Никого не нашлось — попробуй другой запрос или сбрось фильтры" : "Юзеров пока нет"}</Empty>
      ) : (
        <>
          <div className="glass-thin hidden overflow-hidden rounded-2xl md:block">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Юзер</TableHead>
                  <TableHead>Статус</TableHead>
                  <TableHead>Тариф</TableHead>
                  <TableHead>Оплачено до</TableHead>
                  <TableHead>Трафик</TableHead>
                  <TableHead className="text-right">Устройства</TableHead>
                  <TableHead>С нами с</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((u) => (
                  <TableRow key={u.id} className="cursor-pointer" onClick={() => navigate(`/users/${u.id}`)}>
                    <TableCell>
                      <Link to={`/users/${u.id}`} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>
                        {u.telegram_id ?? "—"}
                      </Link>
                      <div className="max-w-60 truncate text-xs text-muted-foreground" title={fmtTelegram(u.telegram_username, u.telegram_name) || undefined}>
                        № {u.id}
                        {(u.telegram_username || u.telegram_name) && ` · ${fmtTelegram(u.telegram_username, u.telegram_name)}`}
                      </div>
                    </TableCell>
                    <TableCell>
                      <UserStatusBadge status={u.status} />
                    </TableCell>
                    <TableCell>{u.plan?.name ?? <span className="text-muted-foreground">нет подписки</span>}</TableCell>
                    <TableCell className="tabular-nums">{fmtDate(u.paid_until)}</TableCell>
                    <TableCell>{u.plan ? <TrafficBar used={u.traffic_used_bytes} limit={u.traffic_limit_bytes} compact /> : <span className="text-muted-foreground">—</span>}</TableCell>
                    <TableCell className="text-right tabular-nums">{u.devices}</TableCell>
                    <TableCell className="text-muted-foreground tabular-nums">{fmtDate(u.created_at)}</TableCell>
                    <TableCell className="w-8">
                      <ChevronRight className="size-4 text-muted-foreground" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <div className="flex flex-col gap-2 md:hidden">
            {users.map((u) => (
              <Link key={u.id} to={`/users/${u.id}`} className="glass-thin flex flex-col gap-3 rounded-2xl p-4 transition-transform duration-200 ease-glide active:scale-[0.985]">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium">{u.telegram_id ?? "—"}</div>
                    {(u.telegram_username || u.telegram_name) && <div className="truncate text-sm text-foreground/85">{fmtTelegram(u.telegram_username, u.telegram_name)}</div>}
                    <div className="text-xs text-muted-foreground">№ {u.id} · с {fmtDate(u.created_at)}</div>
                  </div>
                  <UserStatusBadge status={u.status} />
                </div>
                {u.plan ? (
                  <>
                    <div className="flex justify-between text-sm">
                      <span>{u.plan.name}</span>
                      <span className="text-muted-foreground tabular-nums">до {fmtDate(u.paid_until)}</span>
                    </div>
                    <TrafficBar used={u.traffic_used_bytes} limit={u.traffic_limit_bytes} />
                  </>
                ) : (
                  <div className="text-sm text-muted-foreground">нет подписки</div>
                )}
              </Link>
            ))}
          </div>

          {list.hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button variant="outline" onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>
                {list.isFetchingNextPage && <Loader2 className="animate-spin" />}
                Показать ещё
              </Button>
            </div>
          )}
        </>
      )}
    </>
  )
}
