import { useState } from "react"
import { Link } from "react-router"
import { Route, TriangleAlert } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Section } from "@/components/common/Section"
import { Badge } from "@/components/ui/badge"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { core } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { fmtBytes, fmtDateTime, fmtRelative } from "@/lib/format"
import { useInvalidate, useNodes, useProfiles } from "@/lib/queries"
import { changedCascades, describeRoutingError } from "@/lib/routing"
import type { Node } from "@/lib/types"

export function GeoStatusList({ status }: { status: Node["geo_status"] }) {
  const entries = Object.entries(status ?? {}).sort(([a], [b]) => a.localeCompare(b))
  if (entries.length === 0) return <p className="text-xs text-muted-foreground">своих geo-баз нода не качала</p>
  return (
    <div className="flex flex-col gap-1.5">
      {entries.map(([name, st]) => (
        <div key={name} className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
          <span className="font-mono font-medium">{name}</span>
          {st.sha256 ? (
            <span className="text-xs text-muted-foreground tabular-nums">
              {fmtBytes(st.size ?? 0)} · <span className="font-mono">{st.sha256.slice(0, 12)}</span> · <span title={fmtDateTime(st.downloaded_at)}>{fmtRelative(st.downloaded_at)}</span>
            </span>
          ) : (
            <Badge variant="warning">не скачана</Badge>
          )}
          {st.error && <span className="w-full font-mono text-xs break-words text-[oklch(0.82_0.12_22)]">{st.error}</span>}
        </div>
      ))}
    </div>
  )
}

export function NodeRoutingSection({ node }: { node: Node }) {
  const profiles = useProfiles()
  const invalidate = useInvalidate()
  const [next, setNext] = useState<string | null>(null)
  const current = node.profile_id === null ? "none" : String(node.profile_id)
  const profile = profiles.data?.find((p) => p.id === node.profile_id)
  const target = next === null ? null : next === "none" ? null : profiles.data?.find((p) => String(p.id) === next)
  const exits = next === null ? [] : changedCascades(profile?.doc ?? null, target?.doc ?? null)
  const nodes = useNodes()
  const nodeName = (id: number) => nodes.data?.find((x) => x.id === id)?.name ?? `нода ${id}`

  return (
    <Section title="Маршрутизация" description="куда нода выпускает трафик юзеров">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Select value={current} onValueChange={(v) => v !== current && setNext(v)}>
            <SelectTrigger aria-label="Профиль маршрутизации">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">без профиля — всё напрямую</SelectItem>
              {(profiles.data ?? []).map((p) => (
                <SelectItem key={p.id} value={String(p.id)}>
                  {p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {profile && (
            <Link to={`/routing/${profile.id}`} className="inline-flex w-fit items-center gap-1.5 text-xs text-primary hover:underline">
              <Route className="size-3.5" />
              открыть профиль
            </Link>
          )}
        </div>
        {node.routing_error && (
          <div className="tint-rim flex gap-3 rounded-xl border border-warning/25 bg-warning/[0.07] p-3 text-sm">
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
            <div className="min-w-0">
              <div className="font-medium">Правила по тарифу не встали в xray</div>
              <div className="mt-1 font-mono text-xs break-words text-muted-foreground">{node.routing_error}</div>
            </div>
          </div>
        )}
        <div className="flex flex-col gap-2">
          <div className="text-sm font-medium">Geo-базы на ноде</div>
          <GeoStatusList status={node.geo_status} />
        </div>
      </div>

      <ActionDialog
        open={next !== null}
        onOpenChange={(v) => !v && setNext(null)}
        title={target ? `Профиль «${target.name}» на «${node.name}»` : `Снять профиль с «${node.name}»`}
        consequences={
          <>
            <p>Агент перезапустит xray на ноде — открытые соединения оборвутся.</p>
            {target ? <p>Если профилю нужны свои geo-базы, нода сначала скачает их и только потом применит профиль.</p> : <p>Весь трафик ноды пойдёт напрямую.</p>}
            {exits.length > 0 && <p>Ноды-выходы {exits.map((x) => `«${nodeName(x)}»`).join(", ")} тоже перезапустят xray, если на них самих стоит профиль: у них меняется список служебных ключей.</p>}
          </>
        }
        destructive
        confirmLabel={target ? "Назначить" : "Снять"}
        success={target ? "Профиль назначен" : "Профиль снят"}
        onConfirm={async () => {
          try {
            await core.patch(`/v1/admin/nodes/${node.id}`, { profile_id: target ? target.id : null })
          } catch (err) {
            throw new Error(describeRoutingError(err) ?? describeError(err))
          }
          await invalidate(["nodes"], ["profiles"], ["overview"])
        }}
      />
    </Section>
  )
}
