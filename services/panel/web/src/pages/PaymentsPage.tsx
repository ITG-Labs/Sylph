import { Link, useNavigate, useSearchParams } from "react-router"
import { useInfiniteQuery } from "@tanstack/react-query"
import { ChevronRight, Clock, Loader2, X } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty } from "@/components/common/Section"
import { PaymentStatusBadge, SourceLabel } from "@/components/common/StatusBadges"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { core, qs } from "@/lib/api"
import { fmtDateTime, fmtRub } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { PaymentsPage as PaymentsPageData } from "@/lib/types"

const statuses = [
  ["pending", "Ждут оплаты"],
  ["succeeded", "Оплачены"],
  ["mismatch", "Расхождение"],
  ["rejected", "Отклонены"],
  ["refunded", "Возвращены"],
  ["canceled", "Отменены"],
]

export function PaymentsPage() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const status = params.get("status") ?? "all"
  const source = params.get("source") ?? "all"
  const stuck = params.get("stuck") === "true"
  const userId = params.get("user_id") ?? ""

  const set = (key: string, value: string | null) => {
    const next = new URLSearchParams(params)
    if (value === null || value === "all") next.delete(key)
    else next.set(key, value)
    setParams(next, { replace: true })
  }

  const filters = { status: status === "all" ? "" : status, source: source === "all" ? "" : source, stuck: stuck ? "true" : "", user_id: userId }
  const list = useInfiniteQuery({
    queryKey: ["payments", filters],
    queryFn: ({ pageParam }) => core.get<PaymentsPageData>(`/v1/admin/payments${qs({ ...filters, before: pageParam, limit: 50 })}`),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
  const rows = list.data?.pages.flatMap((p) => p.payments) ?? []

  return (
    <>
      <PageHeader title="Платежи" description="свежие первыми; зависшие — ждут оплаты дольше суток, крон их уже не проверяет" />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Select value={status} onValueChange={(v) => set("status", v)}>
          <SelectTrigger className="w-[calc(50%-4px)] sm:w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все статусы</SelectItem>
            {statuses.map(([v, l]) => (
              <SelectItem key={v} value={v}>
                {l}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={source} onValueChange={(v) => set("source", v)}>
          <SelectTrigger className="w-[calc(50%-4px)] sm:w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Любой источник</SelectItem>
            <SelectItem value="yookassa">ЮKassa</SelectItem>
            <SelectItem value="manual">Вручную</SelectItem>
          </SelectContent>
        </Select>
        <Button variant={stuck ? "default" : "outline"} onClick={() => set("stuck", stuck ? null : "true")} className={cn(!stuck && "text-muted-foreground")}>
          <Clock />
          Зависшие
        </Button>
        {userId && (
          <Button variant="secondary" onClick={() => set("user_id", null)}>
            юзер № {userId}
            <X />
          </Button>
        )}
      </div>

      {list.isPending ? (
        <LoadingBlock rows={6} />
      ) : list.isError ? (
        <LoadError error={list.error} onRetry={() => list.refetch()} />
      ) : rows.length === 0 ? (
        <Empty>{stuck ? "Зависших платежей нет" : "Платежей не нашлось"}</Empty>
      ) : (
        <>
          <div className="hidden overflow-hidden rounded-xl border bg-card md:block">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Платёж</TableHead>
                  <TableHead>Юзер</TableHead>
                  <TableHead>Тариф</TableHead>
                  <TableHead className="text-right">Сумма</TableHead>
                  <TableHead>Статус</TableHead>
                  <TableHead>Создан</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((p) => (
                  <TableRow key={p.id} className="cursor-pointer" onClick={() => navigate(`/payments/${encodeURIComponent(p.payment_id)}`)}>
                    <TableCell>
                      <div className="max-w-56 truncate font-mono text-xs">{p.payment_id}</div>
                      <div className="text-xs">
                        <SourceLabel source={p.source} />
                      </div>
                    </TableCell>
                    <TableCell>
                      <Link to={`/users/${p.user_id}`} className="hover:underline" onClick={(e) => e.stopPropagation()}>
                        {p.telegram_id ?? `№ ${p.user_id}`}
                      </Link>
                    </TableCell>
                    <TableCell>{p.plan_name}</TableCell>
                    <TableCell className="text-right tabular-nums">{fmtRub(p.amount_rub)}</TableCell>
                    <TableCell>
                      <PaymentStatusBadge status={p.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground tabular-nums">{fmtDateTime(p.created_at)}</TableCell>
                    <TableCell className="w-8">
                      <ChevronRight className="size-4 text-muted-foreground" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="flex flex-col gap-2 md:hidden">
            {rows.map((p) => (
              <Link key={p.id} to={`/payments/${encodeURIComponent(p.payment_id)}`} className="flex flex-col gap-2 rounded-xl border bg-card p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium">
                      {p.plan_name} · {fmtRub(p.amount_rub)}
                    </div>
                    <div className="truncate font-mono text-xs text-muted-foreground">{p.payment_id}</div>
                  </div>
                  <PaymentStatusBadge status={p.status} />
                </div>
                <div className="flex justify-between text-xs text-muted-foreground">
                  <span>
                    {p.telegram_id ?? `№ ${p.user_id}`} · <SourceLabel source={p.source} />
                  </span>
                  <span className="tabular-nums">{fmtDateTime(p.created_at)}</span>
                </div>
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
