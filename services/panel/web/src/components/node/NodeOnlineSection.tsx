import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { OnlineDot } from "@/components/common/Online"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { core } from "@/lib/api"
import { fmtDateTime, fmtRelative, fmtTelegram } from "@/lib/format"
import type { NodeOnlineUser } from "@/lib/types"

export function NodeOnlineSection({ nodeId }: { nodeId: number }) {
  const online = useQuery({
    queryKey: ["node-online", nodeId],
    queryFn: () => core.get<NodeOnlineUser[]>(`/v1/admin/nodes/${nodeId}/online`),
    refetchInterval: 30_000,
  })
  return (
    <Section title="Онлайн сейчас" description={online.data ? `${online.data.length} — у кого прямо сейчас открыто соединение через эту ноду` : "у кого прямо сейчас открыто соединение"}>
      {online.isPending ? (
        <LoadingBlock rows={1} />
      ) : online.isError ? (
        <LoadError error={online.error} onRetry={() => online.refetch()} />
      ) : online.data.length === 0 ? (
        <Empty>Сейчас никого</Empty>
      ) : (
        <ul className="well flex flex-col divide-y divide-hairline rounded-xl">
          {online.data.map((u) => (
            <li key={u.user_id} className="flex flex-col gap-1 p-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
              <Link to={`/users/${u.user_id}`} className="flex min-w-0 items-center gap-2 hover:underline">
                <OnlineDot />
                <span className="font-medium">{u.telegram_id ?? `№ ${u.user_id}`}</span>
                {(u.telegram_username || u.telegram_name) && <span className="truncate text-sm text-muted-foreground">{fmtTelegram(u.telegram_username, u.telegram_name)}</span>}
              </Link>
              <div className="flex flex-wrap gap-x-3 gap-y-1 pl-4 sm:pl-0">
                {u.ips.map((ip) => (
                  <span key={ip.ip} className="font-mono text-xs" title={`подключён с ${fmtDateTime(ip.since)}`}>
                    {ip.ip} <span className="font-sans text-muted-foreground">· {fmtRelative(ip.since).replace(" назад", "")}</span>
                  </span>
                ))}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}
