import { useState } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronRight, Plus, TriangleAlert } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { NodeStatusBadge } from "@/components/common/NodeStatusBadge"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty } from "@/components/common/Section"
import { NodeFormDialog } from "@/components/node/NodeDialogs"
import { Button } from "@/components/ui/button"
import { core } from "@/lib/api"
import { fmtRelative, fmtUptime } from "@/lib/format"
import { useGroups, useInvalidate } from "@/lib/queries"
import type { Node } from "@/lib/types"

function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="truncate text-sm tabular-nums">{value}</div>
    </div>
  )
}

export function NodesPage() {
  const groups = useGroups()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const q = useQuery({ queryKey: ["nodes"], queryFn: () => core.get<Node[]>("/v1/admin/nodes"), refetchInterval: 30_000 })
  const groupName = (id: number | null) => (id === null ? "без группы" : (groups.data?.find((g) => g.id === id)?.name ?? `группа ${id}`))

  return (
    <>
      <PageHeader
        title="Ноды"
        description="статус по heartbeat: лежит — тишина больше минуты, с проблемой — синк старше 3 минут или ошибка конфига"
        actions={
          <Button onClick={() => setCreating(true)}>
            <Plus />
            Завести ноду
          </Button>
        }
      />
      {q.isPending ? (
        <LoadingBlock rows={3} />
      ) : q.isError ? (
        <LoadError error={q.error} onRetry={() => q.refetch()} />
      ) : q.data.length === 0 ? (
        <Empty>Нод пока нет — заведи первую</Empty>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {q.data.map((n) => (
            <Link key={n.id} to={`/nodes/${n.id}`} className="group flex flex-col gap-4 rounded-xl border bg-card p-5 transition-colors hover:border-white/15">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate text-lg font-semibold">{n.name}</div>
                  <div className="truncate font-mono text-xs text-muted-foreground">
                    {n.address} · {n.cert_cn}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <NodeStatusBadge status={n.status} />
                  <ChevronRight className="size-4 text-muted-foreground transition group-hover:translate-x-0.5" />
                </div>
              </div>
              <div className="grid grid-cols-3 gap-3">
                <Metric label="юзеров" value={n.active_users ?? "—"} />
                <Metric label="агент" value={n.agent_version ?? "—"} />
                <Metric label="xray" value={n.xray_version ?? "—"} />
                <Metric label="аптайм агента" value={fmtUptime(n.agent_uptime_sec)} />
                <Metric label="аптайм xray" value={fmtUptime(n.xray_uptime_sec)} />
                <Metric label="группа" value={groupName(n.group_id)} />
                <Metric label="heartbeat" value={fmtRelative(n.last_heartbeat)} />
                <Metric label="синк" value={fmtRelative(n.last_sync_at)} />
              </div>
              {n.config_error && (
                <div className="flex gap-2 rounded-lg border border-warning/25 bg-warning/[0.06] p-3 text-sm">
                  <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
                  <span className="min-w-0 font-mono text-xs break-words">{n.config_error}</span>
                </div>
              )}
            </Link>
          ))}
        </div>
      )}
      <NodeFormDialog open={creating} onOpenChange={setCreating} groups={groups.data ?? []} onDone={() => invalidate(["nodes"], ["overview"])} />
    </>
  )
}
