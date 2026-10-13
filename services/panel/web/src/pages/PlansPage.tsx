import { useState } from "react"
import { Link, useNavigate } from "react-router"
import { ChevronRight, Plus } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty } from "@/components/common/Section"
import { PlanBadges } from "@/components/plan/PlanBadges"
import { PlanFormDialog } from "@/components/plan/PlanFormDialog"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { fmtBytes, fmtRub } from "@/lib/format"
import { useGroups, useInvalidate, usePlans } from "@/lib/queries"

export function PlansPage() {
  const plans = usePlans()
  const groups = useGroups()
  const invalidate = useInvalidate()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)
  const groupName = (id: number | null) => (id === null ? "общие" : (groups.data?.find((g) => g.id === id)?.name ?? `группа ${id}`))

  return (
    <>
      <PageHeader
        title="Тарифы"
        description="удалять нельзя — на них ссылаются подписки; ненужный снимают с продажи"
        actions={
          <Button onClick={() => setCreating(true)}>
            <Plus />
            Новый тариф
          </Button>
        }
      />
      {plans.isPending ? (
        <LoadingBlock rows={4} />
      ) : plans.isError ? (
        <LoadError error={plans.error} onRetry={() => plans.refetch()} />
      ) : plans.data.length === 0 ? (
        <Empty>Тарифов пока нет</Empty>
      ) : (
        <>
          <div className="glass-thin hidden overflow-hidden rounded-2xl md:block">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Тариф</TableHead>
                  <TableHead className="text-right">Цена</TableHead>
                  <TableHead className="text-right">Период</TableHead>
                  <TableHead className="text-right">Трафик</TableHead>
                  <TableHead className="text-right">Устройства</TableHead>
                  <TableHead>Ноды</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {plans.data.map((p) => (
                  <TableRow key={p.id} className="cursor-pointer" onClick={() => navigate(`/plans/${p.id}`)}>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium">{p.name}</span>
                        <PlanBadges plan={p} />
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{fmtRub(p.price_rub)}</TableCell>
                    <TableCell className="text-right tabular-nums">{p.period_days} дн</TableCell>
                    <TableCell className="text-right tabular-nums">{fmtBytes(p.traffic_limit_bytes)}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      {p.device_limit ?? "∞"}
                      {p.hwid_required && <span className="ml-1 text-xs text-muted-foreground">HWID</span>}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{groupName(p.location_group_id)}</TableCell>
                    <TableCell className="w-8">
                      <ChevronRight className="size-4 text-muted-foreground" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="flex flex-col gap-2 md:hidden">
            {plans.data.map((p) => (
              <Link key={p.id} to={`/plans/${p.id}`} className="glass-thin flex flex-col gap-2 rounded-2xl p-4 transition-transform duration-200 ease-glide active:scale-[0.985]">
                <div className="flex items-start justify-between gap-2">
                  <span className="font-medium">{p.name}</span>
                  <span className="tabular-nums">{fmtRub(p.price_rub)}</span>
                </div>
                <PlanBadges plan={p} />
                <div className="text-xs text-muted-foreground tabular-nums">
                  {p.period_days} дн · {fmtBytes(p.traffic_limit_bytes)} · устройств {p.device_limit ?? "∞"}
                  {p.hwid_required ? " · HWID" : ""} · {groupName(p.location_group_id)}
                </div>
              </Link>
            ))}
          </div>
        </>
      )}
      <PlanFormDialog open={creating} onOpenChange={setCreating} groups={groups.data ?? []} onDone={(p) => invalidate(["plans"]).then(() => navigate(`/plans/${p.id}`))} />
    </>
  )
}
