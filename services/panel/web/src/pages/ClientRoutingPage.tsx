import { useEffect, useRef, useState } from "react"
import { Link } from "react-router"
import { AnimatePresence, motion } from "motion/react"
import { toast } from "sonner"
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Pencil, Plus, Trash2, TriangleAlert } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { ClientRuleDialog } from "@/components/routing/ClientRuleDialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { core } from "@/lib/api"
import { actionLabels, clientLabels, issuesFor, moveRule, putBody, reasonLabels, ruleKey, type ClientRoutingView, type ClientRule } from "@/lib/clientRouting"
import { ApiError, describeError } from "@/lib/errors"
import { fmtDateTime } from "@/lib/format"
import { useClientRouting, useGeoTags, useNodes } from "@/lib/queries"
import { useQueryClient } from "@tanstack/react-query"

function conditions(r: ClientRule): { label: string; value: string }[] {
  const out: { label: string; value: string }[] = []
  if (r.domain?.length) out.push({ label: "домены", value: r.domain.join(", ") })
  if (r.ip?.length) out.push({ label: "IP", value: r.ip.join(", ") })
  if (r.port) out.push({ label: "порт", value: r.port })
  if (r.network) out.push({ label: "сеть", value: r.network.toUpperCase() })
  return out
}

function fieldIndex(field: string): number | null {
  const m = /^rules\[(\d+)\]/.exec(field)
  return m ? Number(m[1]) : null
}

export function ClientRoutingPage() {
  const routing = useClientRouting()
  const nodes = useNodes()
  const tags = useGeoTags()
  const qc = useQueryClient()
  const [draft, setDraft] = useState<ClientRule[] | null>(null)
  const [dialog, setDialog] = useState<{ index?: number } | null>(null)
  const [saving, setSaving] = useState(false)
  const [fieldError, setFieldError] = useState<{ index: number | null; text: string } | null>(null)
  const seen = useRef<number | null>(null)

  const saved = routing.data
  const dirty = !!saved && !!draft && JSON.stringify(draft.map(ruleKey)) !== JSON.stringify(saved.rules.map(ruleKey))

  useEffect(() => {
    if (!saved || dirty) return
    seen.current = saved.revision
    setDraft(structuredClone(saved.rules))
  }, [saved, dirty])

  if (routing.isPending) return <LoadingBlock rows={4} />
  if (routing.isError) return <LoadError error={routing.error} onRetry={() => routing.refetch()} />
  if (!saved || !draft) return <LoadingBlock rows={4} />

  const rules = draft
  const nodeName = (id?: number) => nodes.data?.find((n) => n.id === id)?.name ?? `нода ${id}`
  const overlapsOf = (i: number) => (dirty ? [] : saved.overlaps.filter((o) => o.rule === i))

  const save = async () => {
    setSaving(true)
    setFieldError(null)
    try {
      const next = await core.put<ClientRoutingView>("/v1/admin/client-routing", putBody(rules, seen.current ?? saved.revision))
      seen.current = next.revision
      qc.setQueryData(["client-routing"], next)
      setDraft(structuredClone(next.rules))
      toast.success("Правила сохранены — клиенты получат их при следующем обновлении подписки")
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error("Правила изменил кто-то другой — обновите страницу")
      } else if (err instanceof ApiError && err.field) {
        setFieldError({ index: fieldIndex(err.field), text: err.message })
        toast.error(describeError(err))
      } else {
        toast.error(describeError(err))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className={dirty ? "pb-24" : undefined}>
      <Link to="/routing" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Маршрутизация
      </Link>
      <PageHeader
        title="Клиентский роутинг"
        description="правила, которые Happ, ITG Ray и SimpleXray получают вместе с подпиской"
        actions={
          <Button onClick={() => setDialog({})} disabled={rules.length >= 64}>
            <Plus />
            Правило
          </Button>
        }
      />

      <div className={`mb-4 flex gap-3 rounded-2xl border p-4 text-sm ${saved.geo.loaded ? "border-white/10" : "border-warning/25 bg-warning/[0.07]"}`}>
        {!saved.geo.loaded && <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />}
        <div>
          {saved.geo.loaded ? (
            <>
              База категорий runetfreedom <span className="font-mono">{saved.geo.release || "—"}</span>, обновлена {fmtDateTime(saved.geo.fetched_at)}
            </>
          ) : (
            "Список категорий ещё не загружен — правила с geosite/geoip сохранить нельзя. cron загружает его раз в сутки и при старте."
          )}
          {saved.updated_at && <div className="mt-1 text-muted-foreground">Правила изменены {fmtDateTime(saved.updated_at)}</div>}
        </div>
      </div>

      <Section title="Правила" description="проверяются сверху вниз; первое подошедшее правило решает, куда пойдёт трафик">
        {rules.length === 0 ? (
          <Empty>Правил нет — клиенты получают подписку без роутинга</Empty>
        ) : (
          <div className="flex flex-col gap-2">
            {rules.map((r, i) => {
              const check = issuesFor(saved, r)
              const err = fieldError && fieldError.index === i ? fieldError.text : null
              return (
                <div key={i} className="well flex flex-col gap-2 rounded-xl p-3 sm:flex-row sm:items-start">
                  <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-white/8 text-xs tabular-nums text-muted-foreground">{i + 1}</span>
                      <span className="text-sm font-medium">{r.name}</span>
                      <ArrowRight className="size-3.5 text-muted-foreground" />
                      <Badge variant={r.action === "block" ? "destructive" : r.action === "direct" ? "success" : "info"}>
                        {r.action === "node" ? `${actionLabels.node} ${nodeName(r.node_id)}` : actionLabels[r.action]}
                      </Badge>
                    </div>
                    <div className="flex flex-wrap gap-1.5 pl-8">
                      {conditions(r).map((c) => (
                        <span key={c.label} className="inline-flex max-w-full items-center gap-1 rounded-full bg-white/6 px-2.5 py-0.5 text-xs">
                          <span className="text-muted-foreground">{c.label}</span>
                          <span className="truncate font-mono">{c.value}</span>
                        </span>
                      ))}
                    </div>
                    <div className="flex flex-col gap-1 pl-8 text-xs">
                      {!check.checked && <span className="text-muted-foreground">совместимость с клиентами проверится после сохранения</span>}
                      {check.issues.map((x) => (
                        <span key={x.client} className={x.skip ? "text-warning" : "text-muted-foreground"}>
                          {clientLabels[x.client]}: {reasonLabels[x.reason] ?? x.reason}
                        </span>
                      ))}
                      {check.missing.length > 0 && <span className="text-destructive">категории пропали из базы: {check.missing.join(", ")} — правило не отправляется</span>}
                      {overlapsOf(i).map((o) => (
                        <span key={`${o.earlier}-${o.value}`} className="text-warning">
                          пересекается с правилом «{saved.rules[o.earlier]?.name}» по <span className="font-mono">{o.value}</span>: в Happ и SimpleXray порядок между действиями не сохраняется
                        </span>
                      ))}
                      {err && <span className="text-destructive">{err}</span>}
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-1 self-end sm:self-center">
                    <Button variant="ghost" size="icon-sm" aria-label="Выше" disabled={i === 0} onClick={() => setDraft(moveRule(rules, i, i - 1))}>
                      <ArrowUp />
                    </Button>
                    <Button variant="ghost" size="icon-sm" aria-label="Ниже" disabled={i === rules.length - 1} onClick={() => setDraft(moveRule(rules, i, i + 1))}>
                      <ArrowDown />
                    </Button>
                    <Button variant="ghost" size="icon-sm" aria-label="Изменить правило" onClick={() => setDialog({ index: i })}>
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-sm" aria-label="Удалить правило" onClick={() => setDraft(rules.filter((_, k) => k !== i))}>
                      <Trash2 />
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </Section>

      <AnimatePresence>
        {dirty && (
          <motion.div
            initial={{ y: 40, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 40, opacity: 0 }}
            transition={{ type: "spring", bounce: 0.25, duration: 0.5 }}
            className="glass-strong fixed inset-x-3 bottom-3 z-40 mx-auto flex max-w-3xl flex-col gap-2 rounded-3xl p-3 sm:flex-row sm:items-center lg:left-[17.5rem]"
          >
            <div className="min-w-0 flex-1 px-2 text-sm">Есть несохранённые изменения</div>
            <div className="flex gap-2 max-sm:justify-end">
              <Button
                variant="ghost"
                onClick={() => {
                  setDraft(structuredClone(saved.rules))
                  setFieldError(null)
                }}
              >
                Отменить
              </Button>
              <Button onClick={save} disabled={saving}>
                Сохранить
              </Button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <ClientRuleDialog
        open={!!dialog}
        onOpenChange={(v) => !v && setDialog(null)}
        rule={dialog?.index !== undefined ? rules[dialog.index] : undefined}
        nodes={nodes.data ?? []}
        tags={tags.data}
        onSave={(r) => {
          if (dialog?.index === undefined) setDraft([...rules, r])
          else setDraft(rules.map((x, k) => (k === dialog.index ? r : x)))
          setFieldError(null)
        }}
      />
    </div>
  )
}
