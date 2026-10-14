import { useState } from "react"
import { Link, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, KeyRound, Pencil, Plus, Power, Trash2, TriangleAlert } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { CopyButton } from "@/components/common/CopyButton"
import { NodeStatusBadge } from "@/components/common/NodeStatusBadge"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { InboundFormDialog, NodeFormDialog } from "@/components/node/NodeDialogs"
import { NodeTrafficSection } from "@/components/node/NodeTrafficSection"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { core } from "@/lib/api"
import { fmtDate, fmtDateTime, fmtRelative, fmtUptime } from "@/lib/format"
import { useGroups, useInvalidate } from "@/lib/queries"
import type { Inbound, Node } from "@/lib/types"

type Dialog = { kind: "edit" } | { kind: "toggle" } | { kind: "newInbound" } | { kind: "editInbound"; inbound: Inbound } | { kind: "rekey"; inbound: Inbound } | { kind: "delete"; inbound: Inbound }

function KV({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[7.5rem_1fr] items-start gap-3 py-1.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className={mono ? "min-w-0 font-mono text-xs break-all" : "min-w-0 break-words"}>{children}</span>
    </div>
  )
}

export function NodePage() {
  const { id } = useParams()
  const groups = useGroups()
  const invalidate = useInvalidate()
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: () => core.get<Node[]>("/v1/admin/nodes"), refetchInterval: 30_000 })
  const inbounds = useQuery({ queryKey: ["inbounds", id], queryFn: () => core.get<Inbound[]>(`/v1/admin/nodes/${id}/inbounds`) })

  if (nodes.isPending) return <LoadingBlock rows={3} />
  if (nodes.isError) return <LoadError error={nodes.error} onRetry={() => nodes.refetch()} />
  const n = nodes.data.find((x) => String(x.id) === id)
  if (!n) return <Empty>Нода не найдена</Empty>
  const close = (v: boolean) => !v && setDialog(null)
  const refresh = () => invalidate(["nodes"], ["inbounds", id], ["overview"])
  const group = n.group_id === null ? "без группы — видна всем тарифам" : (groups.data?.find((g) => g.id === n.group_id)?.name ?? `группа ${n.group_id}`)

  return (
    <>
      <Link to="/nodes" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Ноды
      </Link>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            {n.name}
            <NodeStatusBadge status={n.status} />
          </span>
        }
        description={<span className="font-mono text-xs">{n.address} · CN {n.cert_cn}</span>}
        actions={
          <>
            <Button variant="secondary" onClick={() => setDialog({ kind: "edit" })}>
              <Pencil />
              Изменить
            </Button>
            <Button variant="outline" onClick={() => setDialog({ kind: "toggle" })}>
              <Power />
              {n.enabled ? "Выключить" : "Включить"}
            </Button>
          </>
        }
      />

      {n.config_error && (
        <div className="tint-rim mb-4 flex gap-3 rounded-2xl border border-warning/25 bg-warning/[0.07] p-4 text-sm">
          <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
          <div className="min-w-0">
            <div className="font-medium">Нода не смогла применить конфиг и работает на прошлом</div>
            <div className="mt-1 font-mono text-xs break-words text-muted-foreground">{n.config_error}</div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Section title="Состояние" className="xl:col-span-1">
          <KV label="Группа">{group}</KV>
          <KV label="Юзеров сейчас">{n.active_users ?? "—"}</KV>
          <KV label="Агент">{n.agent_version ?? "—"}</KV>
          <KV label="xray">{n.xray_version ?? "—"}</KV>
          <KV label="Аптайм агента">{fmtUptime(n.agent_uptime_sec)}</KV>
          <KV label="Аптайм xray">{fmtUptime(n.xray_uptime_sec)}</KV>
          <KV label="Heartbeat">
            <span title={fmtDateTime(n.last_heartbeat)}>{fmtRelative(n.last_heartbeat)}</span>
          </KV>
          <KV label="Синк">
            <span title={fmtDateTime(n.last_sync_at)}>{fmtRelative(n.last_sync_at)}</span>
          </KV>
          <KV label="Заведена">{fmtDate(n.created_at)}</KV>
        </Section>

        <Section
          title="Входы"
          description="VLESS + REALITY; из них агент собирает конфиг xray, а subs — ссылки"
          className="xl:col-span-2"
          action={
            <Button size="sm" onClick={() => setDialog({ kind: "newInbound" })}>
              <Plus />
              Вход
            </Button>
          }
        >
          {inbounds.isPending ? (
            <LoadingBlock rows={1} />
          ) : inbounds.isError ? (
            <LoadError error={inbounds.error} />
          ) : inbounds.data.length === 0 ? (
            <Empty>Входов нет — клиентам нечем подключиться</Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {inbounds.data.map((ib) => (
                <div key={ib.id} className="well rounded-xl p-4">
                  <div className="mb-2 flex flex-wrap items-center gap-2">
                    <span className="font-mono text-sm font-medium">{ib.tag}</span>
                    {ib.label && <Badge variant="secondary">{ib.label}</Badge>}
                    <Badge variant="outline">
                      {ib.protocol} · {ib.transport} · {ib.security} · :{ib.port}
                    </Badge>
                    <div className="ml-auto flex gap-1">
                      <Button variant="ghost" size="icon-sm" aria-label="Изменить вход" onClick={() => setDialog({ kind: "editInbound", inbound: ib })}>
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label="Новые ключи" onClick={() => setDialog({ kind: "rekey", inbound: ib })}>
                        <KeyRound />
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label="Удалить вход" onClick={() => setDialog({ kind: "delete", inbound: ib })}>
                        <Trash2 />
                      </Button>
                    </div>
                  </div>
                  <KV label="dest" mono>
                    {ib.reality.dest}
                  </KV>
                  <KV label="SNI" mono>
                    {ib.reality.server_names.join(", ")}
                  </KV>
                  <KV label="shortId" mono>
                    {ib.reality.short_ids.join(", ")}
                  </KV>
                  <KV label="Отпечаток">{ib.reality.fingerprint}</KV>
                  <KV label="flow" mono>
                    {ib.flow}
                  </KV>
                  <KV label="Публичный ключ" mono>
                    <span className="inline-flex items-center gap-1">
                      {ib.reality.public_key}
                      <CopyButton value={ib.reality.public_key} label="Скопировать публичный ключ" />
                    </span>
                  </KV>
                </div>
              ))}
            </div>
          )}
        </Section>
      </div>

      <NodeTrafficSection nodeId={n.id} />

      <NodeFormDialog open={dialog?.kind === "edit"} onOpenChange={close} node={n} groups={groups.data ?? []} onDone={() => refresh()} />
      <ActionDialog
        open={dialog?.kind === "toggle"}
        onOpenChange={close}
        title={n.enabled ? `Выключить «${n.name}»` : `Включить «${n.name}»`}
        consequences={
          n.enabled ? (
            <p>Нода пропадёт из подписок со следующим обновлением, её юзеры переключатся на другие ноды. Сама нода продолжит работать с теми, кто уже подключён.</p>
          ) : (
            <p>Нода снова появится в подписках юзеров её группы.</p>
          )
        }
        destructive={n.enabled}
        confirmLabel={n.enabled ? "Выключить" : "Включить"}
        success={n.enabled ? "Нода выключена" : "Нода включена"}
        onConfirm={async () => {
          await core.patch(`/v1/admin/nodes/${n.id}`, { enabled: !n.enabled })
          await refresh()
        }}
      />
      <InboundFormDialog open={dialog?.kind === "newInbound"} onOpenChange={close} nodeId={n.id} onDone={() => refresh()} />
      <InboundFormDialog
        open={dialog?.kind === "editInbound"}
        onOpenChange={close}
        nodeId={n.id}
        inbound={dialog?.kind === "editInbound" ? dialog.inbound : undefined}
        onDone={() => refresh()}
      />
      <ActionDialog
        open={dialog?.kind === "rekey"}
        onOpenChange={close}
        title="Выпустить новые ключи REALITY"
        description={dialog?.kind === "rekey" ? `Вход ${dialog.inbound.tag}` : undefined}
        consequences={
          <>
            <p>Ядро сгенерирует новую пару ключей, агент перезапустит xray — открытые соединения оборвутся.</p>
            <p className="font-medium">Пока клиент не обновит подписку, он не подключится к этому входу.</p>
          </>
        }
        destructive
        confirmLabel="Новые ключи"
        success="Ключи перевыпущены"
        onConfirm={async () => {
          if (dialog?.kind !== "rekey") return
          await core.patch(`/v1/admin/inbounds/${dialog.inbound.id}`, { reality: { private_key: "" } })
          await refresh()
        }}
      />
      <ActionDialog
        open={dialog?.kind === "delete"}
        onOpenChange={close}
        title="Удалить вход"
        description={dialog?.kind === "delete" ? `${dialog.inbound.tag} · ${dialog.inbound.reality.server_names.join(", ")}` : undefined}
        consequences={
          <>
            <p>Вход пропадёт из конфига ноды и из подписок. Если он последний — к ноде станет нечем подключиться.</p>
            <p>Агент перезапустит xray — все открытые соединения ноды оборвутся.</p>
          </>
        }
        destructive
        confirmLabel="Удалить"
        success="Вход удалён"
        onConfirm={async () => {
          if (dialog?.kind !== "delete") return
          await core.del(`/v1/admin/inbounds/${dialog.inbound.id}`)
          await refresh()
        }}
      />
    </>
  )
}
