import { useEffect, useRef, useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { AnimatePresence, motion } from "motion/react"
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, LayoutTemplate, Pencil, Plus, Trash2, TriangleAlert } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { CopyButton } from "@/components/common/CopyButton"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { BalancerDialog } from "@/components/routing/BalancerDialog"
import { OutboundDialog } from "@/components/routing/OutboundDialog"
import { Field } from "@/components/routing/parts"
import { RuleDialog } from "@/components/routing/RuleDialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { core, qs } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { fmtDateTime, plural } from "@/lib/format"
import { useGeoSources, useInvalidate, useNodes, usePlans } from "@/lib/queries"
import {
  describeRoutingError,
  docProblems,
  isPlanRule,
  kindLabels,
  normalizeDoc,
  orderRules,
  outboundSummary,
  renameTag,
  restartsXray,
  ruleChips,
  ruleTemplates,
  strategies,
  targets,
  usages,
  warningText,
  type Balancer,
  type Outbound,
  type Profile,
  type Rule,
  type RoutingDoc,
} from "@/lib/routing"

type Dialog =
  | { kind: "outbound"; index?: number }
  | { kind: "delOutbound"; index: number }
  | { kind: "balancer"; index?: number }
  | { kind: "delBalancer"; index: number }
  | { kind: "rule"; index?: number }
  | { kind: "save" }
  | { kind: "delete" }

async function loadProfile(id: string): Promise<Profile> {
  const p = await core.get<Profile>(`/v1/admin/profiles/${id}`)
  return { ...p, doc: normalizeDoc(p.doc) }
}

function Target({ doc, tag }: { doc: RoutingDoc; tag: string }) {
  const balancer = doc.balancers.some((b) => b.tag === tag)
  const o = doc.outbounds.find((x) => x.tag === tag)
  if (!balancer && !o) return <Badge variant="destructive">не выбрано</Badge>
  return (
    <Badge variant={o?.kind === "block" ? "destructive" : balancer ? "info" : "default"} className="font-mono">
      {tag}
    </Badge>
  )
}

export function ProfilePage() {
  const { id = "" } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const invalidate = useInvalidate()
  const nodes = useNodes()
  const plans = usePlans()
  const sources = useGeoSources()
  const profile = useQuery({ queryKey: ["profile", id], queryFn: () => loadProfile(id) })
  const [draft, setDraft] = useState<RoutingDoc | null>(null)
  const [name, setName] = useState("")
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [tab, setTab] = useState("setup")

  const saved = profile.data
  const dirty = !!saved && !!draft && (JSON.stringify(draft) !== JSON.stringify(saved.doc) || name.trim() !== saved.name)
  const seen = useRef<number | null>(null)

  useEffect(() => {
    if (!saved || (seen.current === saved.revision && draft)) return
    if (draft && dirty) return
    seen.current = saved.revision
    setDraft(structuredClone(saved.doc))
    setName(saved.name)
  }, [saved, draft, dirty])

  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [dirty])

  if (profile.isPending) return <LoadingBlock rows={4} />
  if (profile.isError) return <LoadError error={profile.error} onRetry={() => profile.refetch()} />
  if (!saved || !draft) return <LoadingBlock rows={4} />

  const doc = draft
  const nodeName = (nid: number) => nodes.data?.find((n) => n.id === nid)?.name ?? `нода ${nid}`
  const planName = (pid: number) => plans.data?.find((p) => p.id === pid)?.name ?? `тариф ${pid}`
  const close = (v: boolean) => !v && setDialog(null)
  const update = (fn: (d: RoutingDoc) => RoutingDoc) => setDraft((d) => (d ? fn(structuredClone(d)) : d))
  const problems = docProblems(doc)
  const general = doc.rules.map((r, i) => ({ r, i })).filter(({ r }) => !isPlanRule(r))
  const byPlan = doc.rules.map((r, i) => ({ r, i })).filter(({ r }) => isPlanRule(r))
  const leastPing = doc.balancers.some((b) => b.strategy === "leastPing")
  const restart = restartsXray(saved.doc, doc)

  const move = (i: number, d: -1 | 1) =>
    update((x) => {
      const j = i + d
      if (j < 0 || j >= x.rules.length || isPlanRule(x.rules[i]) !== isPlanRule(x.rules[j])) return x
      ;[x.rules[i], x.rules[j]] = [x.rules[j], x.rules[i]]
      return x
    })

  const ruleRow = ({ r, i }: { r: Rule; i: number }, first: boolean, last: boolean) => (
    <div key={i} className="well flex flex-col gap-2 rounded-xl p-3 sm:flex-row sm:items-center">
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-white/8 text-xs tabular-nums text-muted-foreground">{i + 1}</span>
          <span className="text-sm font-medium">{r.name || "без названия"}</span>
          <ArrowRight className="size-3.5 text-muted-foreground" />
          <Target doc={doc} tag={r.to} />
        </div>
        <div className="flex flex-wrap gap-1.5 pl-8">
          {ruleChips(r, planName).map((c) => (
            <span key={c.label} className="inline-flex max-w-full items-center gap-1 rounded-full bg-white/6 px-2.5 py-0.5 text-xs">
              <span className="text-muted-foreground">{c.label}</span>
              <span className="truncate font-mono">{c.value}</span>
            </span>
          ))}
        </div>
      </div>
      <div className="flex shrink-0 gap-1 self-end sm:self-center">
        <Button variant="ghost" size="icon-sm" aria-label="Выше" disabled={first} onClick={() => move(i, -1)}>
          <ArrowUp />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label="Ниже" disabled={last} onClick={() => move(i, 1)}>
          <ArrowDown />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label="Изменить правило" onClick={() => setDialog({ kind: "rule", index: i })}>
          <Pencil />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label="Удалить правило" onClick={() => update((x) => ({ ...x, rules: x.rules.filter((_, k) => k !== i) }))}>
          <Trash2 />
        </Button>
      </div>
    </div>
  )

  const outboundDialog = dialog?.kind === "outbound" ? dialog : null
  const balancerDialog = dialog?.kind === "balancer" ? dialog : null
  const ruleDialog = dialog?.kind === "rule" ? dialog : null
  const delOutbound = dialog?.kind === "delOutbound" ? doc.outbounds[dialog.index] : undefined
  const delBalancer = dialog?.kind === "delBalancer" ? doc.balancers[dialog.index] : undefined

  return (
    <div className={dirty ? "pb-24" : undefined}>
      <Link to="/routing" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Маршрутизация
      </Link>
      <PageHeader
        title={saved.name}
        description={
          <>
            {saved.nodes === 0 ? "не назначен ни одной ноде" : `на ${saved.nodes} ${plural(saved.nodes, "ноде", "нодах", "нодах")}`} · изменён {fmtDateTime(saved.updated_at)}
          </>
        }
        actions={
          <>
            <Tabs value={tab} onValueChange={setTab}>
              <TabsList>
                <TabsTrigger value="setup">Настройка</TabsTrigger>
                <TabsTrigger value="json">JSON</TabsTrigger>
              </TabsList>
            </Tabs>
            <Button variant="outline" onClick={() => setDialog({ kind: "delete" })}>
              <Trash2 />
              Удалить
            </Button>
          </>
        }
      />

      {(saved.warnings?.length ?? 0) > 0 && (
        <div className="tint-rim mb-4 flex gap-3 rounded-2xl border border-warning/25 bg-warning/[0.07] p-4 text-sm">
          <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
          <div className="flex min-w-0 flex-col gap-1">
            {saved.warnings!.map((w) => (
              <div key={`${w.outbound}-${w.node_id}`}>{warningText(w, nodeName)}</div>
            ))}
          </div>
        </div>
      )}

      {tab === "json" ? (
        <PreviewPanel profile={saved} nodes={nodes.data ?? []} dirty={dirty} />
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
          <div className="flex min-w-0 flex-col gap-4 xl:col-span-2">
            <Section
              title="Правила"
              description="xray идёт по списку сверху вниз и берёт первое подошедшее"
              action={
                <div className="flex gap-2">
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button size="sm" variant="secondary">
                        <LayoutTemplate />
                        Шаблон
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="max-w-80">
                      {ruleTemplates.map((t) => (
                        <DropdownMenuItem key={t.id} onSelect={() => update((x) => ({ ...x, rules: orderRules([...x.rules, ...t.rules(x)]) }))} className="flex-col items-start gap-0.5">
                          <span className="font-medium">{t.label}</span>
                          <span className="text-xs text-muted-foreground">{t.hint}</span>
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                  <Button size="sm" onClick={() => setDialog({ kind: "rule" })}>
                    <Plus />
                    Правило
                  </Button>
                </div>
              }
            >
              {doc.rules.length === 0 ? (
                <Empty>Правил нет — весь трафик идёт по «всему остальному»</Empty>
              ) : (
                <div className="flex flex-col gap-2">
                  {general.map((x, k) => ruleRow(x, k === 0, k === general.length - 1))}
                  {byPlan.length > 0 && (
                    <>
                      <div className="mt-2 px-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">по тарифу — после остальных, меняются без перезапуска xray</div>
                      {byPlan.map((x, k) => ruleRow(x, k === 0, k === byPlan.length - 1))}
                    </>
                  )}
                </div>
              )}
            </Section>

            <Section
              title="Выходы"
              description="куда правила могут отправить трафик"
              action={
                <Button size="sm" onClick={() => setDialog({ kind: "outbound" })}>
                  <Plus />
                  Выход
                </Button>
              }
            >
              {doc.outbounds.length === 0 ? (
                <Empty>Выходов нет — нужен хотя бы один</Empty>
              ) : (
                <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
                  {doc.outbounds.map((o, i) => (
                    <div key={o.tag} className="well flex items-start gap-2 rounded-xl p-3">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-mono text-sm font-medium">{o.tag}</span>
                          <Badge variant={o.kind === "block" ? "destructive" : o.kind === "cascade" ? "info" : "secondary"}>{kindLabels[o.kind]}</Badge>
                          {o.proxy?.has_secret && <Badge variant="outline">секрет сохранён</Badge>}
                        </div>
                        <div className="mt-1 truncate text-xs text-muted-foreground">{outboundSummary(o, nodeName)}</div>
                      </div>
                      <Button variant="ghost" size="icon-sm" aria-label="Изменить выход" onClick={() => setDialog({ kind: "outbound", index: i })}>
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label="Удалить выход" onClick={() => setDialog({ kind: "delOutbound", index: i })}>
                        <Trash2 />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </Section>
          </div>

          <div className="flex min-w-0 flex-col gap-4">
            <Section title="Профиль">
              <div className="flex flex-col gap-4">
                <Field id="pf-name" label="Название">
                  <Input id="pf-name" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} />
                </Field>
                <div className="flex flex-col gap-2">
                  <div className="text-sm font-medium">Ноды</div>
                  {saved.node_ids.length === 0 ? (
                    <p className="text-xs text-muted-foreground">профиль назначают на странице ноды</p>
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {saved.node_ids.map((nid) => (
                        <Link key={nid} to={`/nodes/${nid}`} className="glass-pill inline-flex h-7 items-center rounded-full px-3 text-sm transition hover:bg-white/16">
                          {nodeName(nid)}
                        </Link>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </Section>

            <Section title="Весь остальной трафик" description="что не подошло ни под одно правило">
              <Select value={doc.default} onValueChange={(v) => update((x) => ({ ...x, default: v }))}>
                <SelectTrigger>
                  <SelectValue placeholder="выбери выход" />
                </SelectTrigger>
                <SelectContent>
                  {targets(doc).map((t) => (
                    <SelectItem key={t.value} value={t.value}>
                      {t.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Section>

            <Section
              title="Балансировщики"
              description="несколько выходов под одним тегом"
              action={
                <Button size="sm" variant="secondary" onClick={() => setDialog({ kind: "balancer" })} disabled={doc.outbounds.length < 2}>
                  <Plus />
                  Балансировщик
                </Button>
              }
            >
              {doc.balancers.length === 0 ? (
                <p className="text-sm text-muted-foreground">нет</p>
              ) : (
                <div className="flex flex-col gap-2">
                  {doc.balancers.map((b, i) => (
                    <div key={b.tag} className="well flex items-start gap-2 rounded-xl p-3">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-mono text-sm font-medium">{b.tag}</span>
                          <Badge variant="info">{strategies.find((s) => s.value === b.strategy)?.label}</Badge>
                        </div>
                        <div className="mt-1 text-xs break-words text-muted-foreground">
                          {b.outbounds.join(" · ")}
                          {b.fallback && ` · запасной ${b.fallback}`}
                        </div>
                      </div>
                      <Button variant="ghost" size="icon-sm" aria-label="Изменить балансировщик" onClick={() => setDialog({ kind: "balancer", index: i })}>
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label="Удалить балансировщик" onClick={() => setDialog({ kind: "delBalancer", index: i })}>
                        <Trash2 />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </Section>

            {leastPing && (
              <Section title="Проверка скорости" description="для «самого быстрого»: xray дёргает ссылку через каждый выход">
                <div className="grid grid-cols-[1fr_6rem] gap-3">
                  <Field id="pr-url" label="Ссылка">
                    <Input id="pr-url" value={doc.probe.url} onChange={(e) => update((x) => ({ ...x, probe: { ...x.probe, url: e.target.value } }))} className="font-mono text-xs" />
                  </Field>
                  <Field id="pr-int" label="Каждые">
                    <Input id="pr-int" value={doc.probe.interval} onChange={(e) => update((x) => ({ ...x, probe: { ...x.probe, interval: e.target.value } }))} className="font-mono text-xs" />
                  </Field>
                </div>
              </Section>
            )}
          </div>
        </div>
      )}

      <AnimatePresence>
        {dirty && (
          <motion.div
            initial={{ y: 40, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 40, opacity: 0 }}
            transition={{ type: "spring", bounce: 0.25, duration: 0.5 }}
            className="glass-strong fixed inset-x-3 bottom-3 z-40 mx-auto flex max-w-3xl flex-col gap-2 rounded-3xl p-3 sm:flex-row sm:items-center lg:left-[17.5rem]"
          >
            <div className="min-w-0 flex-1 px-2 text-sm">
              {problems.length > 0 ? <span className="text-warning">{problems[0]}</span> : <span>Есть несохранённые изменения</span>}
            </div>
            <div className="flex gap-2 max-sm:justify-end">
              <Button
                variant="ghost"
                onClick={() => {
                  setDraft(structuredClone(saved.doc))
                  setName(saved.name)
                }}
              >
                Отменить
              </Button>
              <Button onClick={() => setDialog({ kind: "save" })} disabled={problems.length > 0 || name.trim() === ""}>
                Сохранить
              </Button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <OutboundDialog
        open={!!outboundDialog}
        onOpenChange={close}
        doc={doc}
        outbound={outboundDialog?.index !== undefined ? doc.outbounds[outboundDialog.index] : undefined}
        nodes={nodes.data ?? []}
        profileNodes={saved.node_ids}
        onSave={(o: Outbound) =>
          update((x) => {
            if (outboundDialog?.index === undefined) return { ...x, outbounds: [...x.outbounds, o], default: x.default || o.tag }
            const old = x.outbounds[outboundDialog.index].tag
            const next = renameTag(x, old, o.tag)
            next.outbounds = x.outbounds.map((e, k) => (k === outboundDialog.index ? o : e))
            return next
          })
        }
      />
      <ActionDialog
        open={!!delOutbound}
        onOpenChange={close}
        title={`Удалить выход ${delOutbound?.tag ?? ""}`}
        consequences={delOutbound && usages(doc, delOutbound.tag).length > 0 ? <p>Он нужен: {usages(doc, delOutbound.tag).join(", ")}. Сначала перенаправь их.</p> : undefined}
        destructive
        canConfirm={!!delOutbound && usages(doc, delOutbound.tag).length === 0}
        confirmLabel="Удалить"
        onConfirm={async () => {
          if (dialog?.kind !== "delOutbound") return
          const i = dialog.index
          update((x) => ({ ...x, outbounds: x.outbounds.filter((_, k) => k !== i) }))
        }}
      />
      <BalancerDialog
        open={!!balancerDialog}
        onOpenChange={close}
        doc={doc}
        balancer={balancerDialog?.index !== undefined ? doc.balancers[balancerDialog.index] : undefined}
        onSave={(b: Balancer) =>
          update((x) => {
            if (balancerDialog?.index === undefined) return { ...x, balancers: [...x.balancers, b] }
            const next = renameTag(x, x.balancers[balancerDialog.index].tag, b.tag)
            next.balancers = next.balancers.map((e, k) => (k === balancerDialog.index ? b : e))
            return next
          })
        }
      />
      <ActionDialog
        open={!!delBalancer}
        onOpenChange={close}
        title={`Удалить балансировщик ${delBalancer?.tag ?? ""}`}
        consequences={delBalancer && usages(doc, delBalancer.tag).length > 0 ? <p>Он нужен: {usages(doc, delBalancer.tag).join(", ")}. Сначала перенаправь их.</p> : undefined}
        destructive
        canConfirm={!!delBalancer && usages(doc, delBalancer.tag).length === 0}
        confirmLabel="Удалить"
        onConfirm={async () => {
          if (dialog?.kind !== "delBalancer") return
          const i = dialog.index
          update((x) => ({ ...x, balancers: x.balancers.filter((_, k) => k !== i) }))
        }}
      />
      <RuleDialog
        open={!!ruleDialog}
        onOpenChange={close}
        doc={doc}
        rule={ruleDialog?.index !== undefined ? doc.rules[ruleDialog.index] : undefined}
        plans={plans.data ?? []}
        sources={sources.data ?? []}
        onSave={(r: Rule) =>
          update((x) => ({
            ...x,
            rules: orderRules(ruleDialog?.index === undefined ? [...x.rules, r] : x.rules.map((e, k) => (k === ruleDialog.index ? r : e))),
          }))
        }
      />
      <ActionDialog
        open={dialog?.kind === "save"}
        onOpenChange={close}
        title={`Сохранить «${name.trim()}»`}
        consequences={
          saved.nodes === 0 ? undefined : restart ? (
            <>
              <p>
                {saved.nodes} {plural(saved.nodes, "нода перезапустит", "ноды перезапустят", "нод перезапустят")} xray — открытые соединения на {saved.nodes === 1 ? "ней" : "них"} оборвутся.
              </p>
              <p>Если нужной geo-базы на ноде ещё нет, нода подождёт её и применит профиль, когда скачает.</p>
            </>
          ) : (
            <p>Меняются только правила по тарифу — ноды подхватят их без перезапуска.</p>
          )
        }
        destructive={saved.nodes > 0 && restart}
        confirmLabel="Сохранить"
        success="Профиль сохранён"
        onConfirm={async () => {
          try {
            const next = await core.put<Profile>(`/v1/admin/profiles/${saved.id}`, { name: name.trim(), doc, revision: saved.revision })
            const fresh = { ...next, doc: normalizeDoc(next.doc) }
            seen.current = fresh.revision
            setDraft(structuredClone(fresh.doc))
            setName(fresh.name)
            qc.setQueryData(["profile", id], fresh)
            await invalidate(["profiles"], ["nodes"], ["profile-preview", id])
          } catch (err) {
            throw new Error(describeRoutingError(err, doc) ?? describeError(err))
          }
        }}
      />
      <ActionDialog
        open={dialog?.kind === "delete"}
        onOpenChange={close}
        title={`Удалить профиль «${saved.name}»`}
        consequences={saved.nodes > 0 ? <p>Профиль назначен {saved.nodes} {plural(saved.nodes, "ноде", "нодам", "нодам")} — сначала переключи их на другой профиль или сними.</p> : <p>Профиль ни на одной ноде, удаление ничего не задевает.</p>}
        destructive
        canConfirm={saved.nodes === 0}
        confirmLabel="Удалить"
        success="Профиль удалён"
        onConfirm={async () => {
          await core.del(`/v1/admin/profiles/${saved.id}`)
          await invalidate(["profiles"])
          navigate("/routing")
        }}
      />
    </div>
  )
}

function PreviewPanel({ profile, nodes, dirty }: { profile: Profile; nodes: { id: number; name: string }[]; dirty: boolean }) {
  const [nodeId, setNodeId] = useState(profile.node_ids[0] ? String(profile.node_ids[0]) : "none")
  const preview = useQuery({
    queryKey: ["profile-preview", String(profile.id), nodeId, profile.revision],
    queryFn: () => core.get<unknown>(`/v1/admin/profiles/${profile.id}/preview${qs({ node_id: nodeId === "none" ? null : nodeId })}`),
  })
  const text = preview.data ? JSON.stringify(preview.data, null, 2) : ""
  return (
    <Section
      title="Что получит xray"
      description={dirty ? "сохранённая версия — несохранённые правки сюда не попали; секреты скрыты" : "секреты скрыты; правила по тарифу агент держит отдельно — они в plan_rules"}
      action={
        <div className="flex items-center gap-2">
          <Select value={nodeId} onValueChange={setNodeId}>
            <SelectTrigger className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">без ноды</SelectItem>
              {nodes.map((n) => (
                <SelectItem key={n.id} value={String(n.id)}>
                  {n.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {text && <CopyButton value={text} label="Скопировать JSON" />}
        </div>
      }
    >
      {preview.isPending ? (
        <LoadingBlock rows={2} />
      ) : preview.isError ? (
        <LoadError error={preview.error} onRetry={() => preview.refetch()} />
      ) : (
        <pre className="well max-h-[70vh] overflow-auto rounded-xl p-4 font-mono text-xs leading-relaxed">{text}</pre>
      )}
    </Section>
  )
}
