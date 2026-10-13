import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router"
import { useInfiniteQuery } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty } from "@/components/common/Section"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { core, qs } from "@/lib/api"
import { auditAction, auditDetails, auditEntities, auditEntity, auditLink } from "@/lib/audit"
import { fmtDateTime } from "@/lib/format"
import type { AuditPage as AuditPageData } from "@/lib/types"

export function AuditPage() {
  const [params, setParams] = useSearchParams()
  const entity = params.get("entity") ?? "all"
  const [entityId, setEntityId] = useState(params.get("entity_id") ?? "")

  useEffect(() => {
    const t = setTimeout(() => {
      const next = new URLSearchParams(params)
      if (entityId.trim()) next.set("entity_id", entityId.trim())
      else next.delete("entity_id")
      if (next.toString() !== params.toString()) setParams(next, { replace: true })
    }, 350)
    return () => clearTimeout(t)
  }, [entityId, params, setParams])

  const filters = { entity: entity === "all" ? "" : entity, entity_id: params.get("entity_id") ?? "" }
  const list = useInfiniteQuery({
    queryKey: ["audit", "list", filters],
    queryFn: ({ pageParam }) => core.get<AuditPageData>(`/v1/admin/audit${qs({ ...filters, before: pageParam, limit: 50 })}`),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
  const rows = list.data?.pages.flatMap((p) => p.entries) ?? []

  return (
    <>
      <PageHeader title="Аудит" description="каждое успешное изменение из панели: кто, когда и что прислал (без секретов)" />
      <div className="mb-4 flex flex-col gap-2 sm:flex-row">
        <Select
          value={entity}
          onValueChange={(v) => {
            const next = new URLSearchParams(params)
            if (v === "all") next.delete("entity")
            else next.set("entity", v)
            setParams(next, { replace: true })
          }}
        >
          <SelectTrigger className="h-10 sm:w-80">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все изменения</SelectItem>
            {auditEntities.map(([v, l]) => (
              <SelectItem key={v} value={v}>
                {l}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input className="h-10 sm:w-48" placeholder="id сущности" value={entityId} onChange={(e) => setEntityId(e.target.value)} />
      </div>

      {list.isPending ? (
        <LoadingBlock rows={6} />
      ) : list.isError ? (
        <LoadError error={list.error} onRetry={() => list.refetch()} />
      ) : rows.length === 0 ? (
        <Empty>Записей нет</Empty>
      ) : (
        <>
          <ol className="glass-thin flex flex-col overflow-hidden rounded-2xl">
            {rows.map((e) => {
              const link = auditLink(e.entity, e.entity_id)
              const details = auditDetails(e.details)
              return (
                <li key={e.id} className="flex flex-col gap-1 border-b border-white/6 px-4 py-3 transition-colors last:border-0 hover:bg-white/[0.035] sm:flex-row sm:items-start sm:gap-4 sm:px-5">
                  <div className="w-36 shrink-0 text-xs text-muted-foreground tabular-nums sm:pt-0.5">{fmtDateTime(e.at)}</div>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm">
                      <span className="font-medium">{e.operator || "без имени"}</span> {auditAction(e.entity, e.action)}
                      {e.entity_id && (
                        <>
                          {" · "}
                          {link ? (
                            <Link to={link} className="text-primary hover:underline">
                              {auditEntity(e.entity)} {e.entity_id}
                            </Link>
                          ) : (
                            <span className="text-muted-foreground">
                              {auditEntity(e.entity)} {e.entity_id}
                            </span>
                          )}
                        </>
                      )}
                    </div>
                    {details && <div className="mt-1 truncate font-mono text-xs text-muted-foreground" title={details}>{details}</div>}
                  </div>
                  <div className="hidden shrink-0 font-mono text-[11px] text-muted-foreground/70 lg:block">{e.route}</div>
                </li>
              )
            })}
          </ol>
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
