import { useEffect, useState } from "react"
import { Link } from "react-router"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { OnlineDot } from "@/components/common/Online"
import { LoadError } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { core, qs } from "@/lib/api"
import { fmtDateTime, fmtRelative } from "@/lib/format"
import type { IPHistoryRow, Presence } from "@/lib/types"

const pageSize = 20

export function UserOnlineSection({ userId, presence }: { userId: number; presence: Presence }) {
  const [page, setPage] = useState(0)
  const history = useQuery({
    queryKey: ["user-ips", userId, page],
    queryFn: () => core.get<{ items: IPHistoryRow[]; total: number }>(`/v1/admin/users/${userId}/ips${qs({ limit: pageSize, offset: page * pageSize })}`),
    refetchInterval: 30_000,
    placeholderData: keepPreviousData,
  })
  const pages = Math.max(1, Math.ceil((history.data?.total ?? 0) / pageSize))
  useEffect(() => {
    if (history.data && page >= pages) setPage(pages - 1)
  }, [history.data, page, pages])

  return (
    <Section
      title="Онлайн"
      description={
        presence.online
          ? "сейчас подключён"
          : presence.last_seen
            ? `был в сети ${fmtRelative(presence.last_seen)}${presence.last_node ? ` · ${presence.last_node.name}` : ""}`
            : "ещё ни разу не подключался"
      }
    >
      <div className="flex flex-col gap-4">
        {presence.now.length > 0 && (
          <ul className="well flex flex-col divide-y divide-hairline rounded-xl">
            {presence.now.map((n) => (
              <li key={n.node_id} className="flex flex-col gap-1.5 p-3">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <OnlineDot />
                  <Link to={`/nodes/${n.node_id}`} className="hover:underline">
                    {n.node_name}
                  </Link>
                </div>
                <div className="flex flex-wrap gap-x-4 gap-y-1 pl-4 text-sm">
                  {n.ips.map((ip) => (
                    <span key={ip.ip} className="font-mono text-xs" title={`подключён с ${fmtDateTime(ip.since)}`}>
                      {ip.ip} <span className="font-sans text-muted-foreground">· {fmtRelative(ip.since).replace(" назад", "")}</span>
                    </span>
                  ))}
                </div>
              </li>
            ))}
          </ul>
        )}
        <div>
          <div className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">История IP</div>
          {history.isError ? (
            <LoadError error={history.error} onRetry={() => history.refetch()} />
          ) : (history.data?.items.length ?? 0) === 0 ? (
            <Empty>{history.isPending ? "Загружаю…" : "Пока пусто — IP появятся после первого подключения"}</Empty>
          ) : (
            <>
              <div className="well overflow-x-auto rounded-xl">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead>IP</TableHead>
                      <TableHead>Нода</TableHead>
                      <TableHead>Впервые</TableHead>
                      <TableHead>Последний раз</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {history.data?.items.map((r) => (
                      <TableRow key={`${r.node_id}-${r.ip}`}>
                        <TableCell className="font-mono text-xs">{r.ip}</TableCell>
                        <TableCell>{r.node_name}</TableCell>
                        <TableCell className="text-muted-foreground tabular-nums">{fmtDateTime(r.first_seen)}</TableCell>
                        <TableCell className="tabular-nums">{fmtDateTime(r.last_seen)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
              {pages > 1 && (
                <div className="mt-2 flex items-center justify-end gap-2 text-xs text-muted-foreground">
                  <Button variant="ghost" size="sm" disabled={page === 0} onClick={() => setPage((p) => p - 1)}>
                    Новее
                  </Button>
                  <span className="tabular-nums">
                    {page + 1} / {pages}
                  </span>
                  <Button variant="ghost" size="sm" disabled={page + 1 >= pages} onClick={() => setPage((p) => p + 1)}>
                    Старше
                  </Button>
                </div>
              )}
            </>
          )}
          <p className="mt-2 text-xs text-muted-foreground">IP хранятся 30 дней (по умолчанию, SYLPH_CRON_IP_RETENTION_DAYS у крона) и видны только в панели.</p>
        </div>
      </div>
    </Section>
  )
}
