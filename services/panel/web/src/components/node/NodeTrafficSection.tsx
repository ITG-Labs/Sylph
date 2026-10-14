import { useState } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { TrafficChart } from "@/components/node/TrafficChart"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { core } from "@/lib/api"
import { fmtBytes, fmtTelegram } from "@/lib/format"
import type { NodeTraffic } from "@/lib/types"

type Win = "today" | "days_7" | "days_30"

const windows: { key: Win; label: string; days: number; top: string }[] = [
  { key: "today", label: "Сегодня", days: 1, top: "за сегодня" },
  { key: "days_7", label: "7 дней", days: 7, top: "за 7 дней" },
  { key: "days_30", label: "30 дней", days: 30, top: "за 30 дней" },
]

export function NodeTrafficSection({ nodeId }: { nodeId: number }) {
  const [win, setWin] = useState<Win>("today")
  const stats = useQuery({
    queryKey: ["node-traffic", nodeId],
    queryFn: () => core.get<NodeTraffic>(`/v1/admin/nodes/${nodeId}/stats`),
    refetchInterval: 60_000,
  })
  const w = windows.find((x) => x.key === win) ?? windows[0]

  return (
    <Section title="Трафик" description="вверх + вниз, сутки по Москве" className="mt-4">
      {stats.isPending ? (
        <LoadingBlock rows={2} />
      ) : stats.isError ? (
        <LoadError error={stats.error} onRetry={() => stats.refetch()} />
      ) : stats.data.days_30.up + stats.data.days_30.down === 0 ? (
        <Empty>За 30 дней через ноду не прошло ни байта</Empty>
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <div className="flex min-w-0 flex-col gap-4">
            <Tabs value={win} onValueChange={(v) => setWin(v as Win)}>
              <TabsList>
                {windows.map((x) => (
                  <TabsTrigger key={x.key} value={x.key}>
                    {x.label}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            <div>
              <div className="text-3xl font-semibold tracking-tight tabular-nums">{fmtBytes(stats.data[win].up + stats.data[win].down)}</div>
              <div className="mt-0.5 text-sm text-muted-foreground tabular-nums">
                ↓ {fmtBytes(stats.data[win].down)} · ↑ {fmtBytes(stats.data[win].up)}
              </div>
            </div>
            <TrafficChart days={stats.data.daily} highlight={w.days} />
          </div>
          <div className="flex min-w-0 flex-col gap-2">
            <div className="text-sm font-medium">
              Больше всех <span className="font-normal text-muted-foreground">{w.top}</span>
            </div>
            {stats.data.top[win].length === 0 ? (
              <Empty>Никого</Empty>
            ) : (
              <ol className="flex flex-col">
                {stats.data.top[win].map((u, i) => {
                  const tg = fmtTelegram(u.telegram_username, u.telegram_name)
                  return (
                    <li key={u.user_id}>
                      <Link
                        to={`/users/${u.user_id}`}
                        className="-mx-2 grid grid-cols-[1.25rem_minmax(0,1fr)_auto] items-baseline gap-2 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-white/6"
                      >
                        <span className="text-right text-xs text-muted-foreground tabular-nums">{i + 1}</span>
                        <span className="min-w-0 truncate">
                          <span className="font-medium tabular-nums">{u.telegram_id ?? `№ ${u.user_id}`}</span>
                          {tg && <span className="text-muted-foreground"> · {tg}</span>}
                        </span>
                        <span className="tabular-nums">{fmtBytes(u.bytes)}</span>
                      </Link>
                    </li>
                  )
                })}
              </ol>
            )}
          </div>
        </div>
      )}
    </Section>
  )
}
