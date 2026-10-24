import { useState } from "react"
import { Link } from "react-router"
import { Trash2, TriangleAlert } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { deletable, holderLabel, statusOf } from "@/lib/addresses"
import { core } from "@/lib/api"
import { fmtDateTime, fmtRelative } from "@/lib/format"
import { useAddresses, useInvalidate } from "@/lib/queries"
import type { NodeAddress } from "@/lib/types"

export function NodeAddressesSection({ nodeId }: { nodeId: number }) {
  const addresses = useAddresses()
  const invalidate = useInvalidate()
  const [deleting, setDeleting] = useState<NodeAddress | null>(null)
  const list = addresses.data?.filter((a) => a.node_id === nodeId) ?? []

  return (
    <Section title="Дополнительные IP" description="для выделенных адресов юзеров; добавляются на самой ноде командой sudo ./sylph ip add <IPv4>">
      {addresses.isPending ? (
        <LoadingBlock rows={1} />
      ) : addresses.isError ? (
        <LoadError error={addresses.error} />
      ) : list.length === 0 ? (
        <Empty>Доп. адресов нет — нода сообщит их сама, когда их подключат</Empty>
      ) : (
        <div className="flex flex-col gap-2">
          {list.some((a) => a.no_direct_warning) && (
            <div className="tint-rim flex gap-3 rounded-xl border border-warning/25 bg-warning/[0.07] p-3 text-sm">
              <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
              <span>Профиль маршрутизации ноды никуда не выпускает трафик напрямую — выделенные адреса на ней не используются, сайты видят адреса прокси и каскадов.</span>
            </div>
          )}
          {list.some((a) => a.balancer_warning) && (
            <div className="tint-rim flex gap-3 rounded-xl border border-warning/25 bg-warning/[0.07] p-3 text-sm">
              <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
              <span>В профиле маршрутизации ноды часть трафика уходит в балансировщик с прямым выходом — эта часть у владельцев адресов пойдёт через общий адрес ноды.</span>
            </div>
          )}
          <ul className="flex flex-col gap-2">
            {list.map((a) => {
              const st = statusOf(a)
              return (
                <li key={a.id} className="well flex flex-col gap-2 rounded-xl p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-sm font-medium">{a.ip}</span>
                    <Badge variant={st.variant}>{st.label}</Badge>
                    <span className="ml-auto text-sm">
                      {a.assignment ? (
                        <Link to={`/users/${a.assignment.user_id}`} className="hover:underline">
                          {holderLabel(a.assignment)}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">свободен</span>
                      )}
                    </span>
                    {deletable(a) && (
                      <Button variant="ghost" size="icon-sm" aria-label="Удалить адрес" onClick={() => setDeleting(a)}>
                        <Trash2 />
                      </Button>
                    )}
                  </div>
                  {a.assignment?.note && <div className="text-xs text-muted-foreground">{a.assignment.note}</div>}
                  {a.error && <div className="font-mono text-xs break-words text-destructive">{a.error}</div>}
                  <div className="text-xs text-muted-foreground" title={fmtDateTime(a.last_seen_at)}>
                    нода сообщала {fmtRelative(a.last_seen_at)}
                  </div>
                </li>
              )
            })}
          </ul>
        </div>
      )}
      <ActionDialog
        open={deleting !== null}
        onOpenChange={(v) => !v && setDeleting(null)}
        title="Удалить адрес"
        description={deleting ? deleting.ip : undefined}
        consequences={
          deleting?.assignment ? (
            <p>Адрес закреплён за {holderLabel(deleting.assignment)} — привязка удалится вместе с ним.</p>
          ) : (
            <p>Нода этот адрес не сообщает или давно молчит; запись просто уберётся из списка. Если нода вернётся с этим адресом, он появится снова.</p>
          )
        }
        destructive={!!deleting?.assignment}
        confirmLabel="Удалить"
        success="Адрес удалён"
        onConfirm={async () => {
          if (!deleting) return
          await core.del(`/v1/admin/addresses/${deleting.id}`)
          await invalidate(["addresses"], ["user"])
        }}
      />
    </Section>
  )
}
