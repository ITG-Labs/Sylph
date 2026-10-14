import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { ArrowLeft, Pencil, Plus, Trash2, X } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { NodeStatusBadge } from "@/components/common/NodeStatusBadge"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { MoveConsequences } from "@/components/group/MoveConsequences"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { core } from "@/lib/api"
import { fmtDate, fmtRub, plural } from "@/lib/format"
import { describeNodeMove, describePlanMove, hiddenReason, isHarsh, nodeMoveImpact, planMoveImpact, type ImpactLine, type PlanLike } from "@/lib/groups"
import { useGroups, useInvalidate, useNodes, usePlans } from "@/lib/queries"
import type { Node, Plan } from "@/lib/types"

type PlanRow = Plan & PlanLike

type Dialog =
  | { kind: "rename" }
  | { kind: "delete" }
  | { kind: "addNode" }
  | { kind: "addPlan" }
  | { kind: "removeNode"; node: Node }
  | { kind: "removePlan"; plan: PlanRow }

const quoted = (names: string[]) => names.map((n) => `«${n}»`).join(", ")

function Picker<T extends { id: number }>({
  items,
  value,
  onChange,
  label,
  render,
}: {
  items: T[]
  value: number | null
  onChange: (id: number) => void
  label: string
  render: (item: T) => React.ReactNode
}) {
  return (
    <div role="radiogroup" aria-label={label} className="well flex max-h-64 flex-col gap-0.5 overflow-y-auto rounded-xl p-1.5">
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          role="radio"
          aria-checked={value === item.id}
          onClick={() => onChange(item.id)}
          className="flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors duration-150 hover:bg-white/6 focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:outline-none aria-checked:bg-primary/16 aria-checked:shadow-[inset_0_0_0_1px_oklch(0.74_0.15_287/45%)]"
        >
          {render(item)}
        </button>
      ))}
    </div>
  )
}

export function GroupPage() {
  const { id } = useParams()
  const gid = Number(id)
  const navigate = useNavigate()
  const groups = useGroups()
  const nodes = useNodes()
  const plans = usePlans()
  const invalidate = useInvalidate()
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [pick, setPick] = useState<number | null>(null)
  const [name, setName] = useState("")

  const query = [groups, nodes, plans].find((q) => q.isError)
  if (query) return <LoadError error={query.error} onRetry={() => query.refetch()} />
  if (!groups.data || !nodes.data || !plans.data) return <LoadingBlock rows={4} />

  const g = groups.data.find((x) => x.id === gid)
  if (!g) return <Empty>Группа не найдена</Empty>
  const allNodes = nodes.data
  const allPlans: PlanRow[] = plans.data.map((p) => ({ ...p, active_users: p.active_users ?? 0 }))
  const gNodes = allNodes.filter((n) => n.group_id === gid)
  const gPlans = allPlans.filter((p) => p.location_group_id === gid)
  const where = (group: number | null) => (group === null ? null : (groups.data.find((x) => x.id === group)?.name ?? `группа ${group}`))
  const refresh = () => invalidate(["nodes"], ["plans"], ["groups"], ["overview"])
  const open = (d: Dialog) => {
    setPick(null)
    setName(g.name)
    setDialog(d)
  }
  const close = (v: boolean) => !v && setDialog(null)

  const pickedNode = dialog?.kind === "addNode" ? allNodes.find((n) => n.id === pick) : undefined
  const pickedPlan = dialog?.kind === "addPlan" ? allPlans.find((p) => p.id === pick) : undefined
  let lines: ImpactLine[] = []
  if (pickedNode) lines = describeNodeMove(pickedNode, gid, nodeMoveImpact(pickedNode, gid, allPlans, allNodes))
  if (pickedPlan) lines = describePlanMove(pickedPlan, gid, planMoveImpact(pickedPlan, gid, allNodes))
  if (dialog?.kind === "removeNode") lines = describeNodeMove(dialog.node, null, nodeMoveImpact(dialog.node, null, allPlans, allNodes))
  if (dialog?.kind === "removePlan") lines = describePlanMove(dialog.plan, null, planMoveImpact(dialog.plan, null, allNodes))
  const consequences = lines.length > 0 ? <MoveConsequences lines={lines} /> : undefined
  const empty = gNodes.length === 0 && gPlans.length === 0

  return (
    <>
      <Link to="/groups" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Группы
      </Link>
      <PageHeader
        title={g.name}
        description={`создана ${fmtDate(g.created_at)} · тарифы группы видят её ноды и общие ноды`}
        actions={
          <>
            <Button variant="secondary" onClick={() => open({ kind: "rename" })}>
              <Pencil />
              Переименовать
            </Button>
            <Button variant="outline" onClick={() => open({ kind: "delete" })}>
              <Trash2 />
              Удалить
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Section
          title="Ноды"
          description="видны только тарифам этой группы"
          action={
            <Button size="sm" onClick={() => open({ kind: "addNode" })}>
              <Plus />
              Добавить
            </Button>
          }
        >
          {gNodes.length === 0 ? (
            <Empty>Нод нет — тарифы группы видят только общие ноды</Empty>
          ) : (
            <div className="flex flex-col gap-2">
              {gNodes.map((n) => (
                <div key={n.id} className="well flex items-center gap-3 rounded-xl py-2 pr-2 pl-3.5">
                  <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2.5 gap-y-1">
                    <Link to={`/nodes/${n.id}`} className="truncate font-medium hover:underline">
                      {n.name}
                    </Link>
                    <NodeStatusBadge status={n.status} />
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => open({ kind: "removeNode", node: n })}>
                    <X />
                    Убрать
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Section>

        <Section
          title="Тарифы"
          description="их юзеры видят ноды группы и общие ноды"
          action={
            <Button size="sm" onClick={() => open({ kind: "addPlan" })}>
              <Plus />
              Добавить
            </Button>
          }
        >
          {gPlans.length === 0 ? (
            <Empty>Тарифов нет — ноды группы сейчас никому не видны</Empty>
          ) : (
            <div className="flex flex-col gap-2">
              {gPlans.map((p) => (
                <div key={p.id} className="well flex items-center gap-3 rounded-xl py-2 pr-2 pl-3.5">
                  <div className="min-w-0 flex-1">
                    <Link to={`/plans/${p.id}`} className="block truncate font-medium hover:underline">
                      {p.name}
                    </Link>
                    <div className="text-xs text-muted-foreground tabular-nums">
                      {p.active_users} {plural(p.active_users, "юзер", "юзера", "юзеров")} сейчас · {fmtRub(p.price_rub)}
                    </div>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => open({ kind: "removePlan", plan: p })}>
                    <X />
                    Убрать
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Section>
      </div>

      <ActionDialog
        open={dialog?.kind === "rename"}
        onOpenChange={close}
        title="Переименовать группу"
        description="Имя группы видно только в панели — юзеры его не видят"
        confirmLabel="Сохранить"
        canConfirm={name.trim() !== "" && name.trim() !== g.name}
        success="Группа переименована"
        onConfirm={async () => {
          await core.patch(`/v1/admin/groups/${g.id}`, { name: name.trim() })
          await refresh()
        }}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="gname">Название</Label>
          <Input id="gname" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoFocus />
        </div>
      </ActionDialog>

      <ActionDialog
        open={dialog?.kind === "delete"}
        onOpenChange={close}
        title={empty ? `Удалить группу «${g.name}»` : "Группу пока не удалить"}
        consequences={
          empty ? (
            <p>Группа пустая — ни нод, ни тарифов в ней нет, юзеров это не коснётся.</p>
          ) : (
            <>
              <p>Удалить можно только пустую группу. Сначала убери или перенеси в другую группу:</p>
              {gNodes.length > 0 && <p>ноды {quoted(gNodes.map((n) => n.name))};</p>}
              {gPlans.length > 0 && <p>тарифы {quoted(gPlans.map((p) => p.name))}.</p>}
            </>
          )
        }
        destructive
        confirmLabel="Удалить"
        canConfirm={empty}
        success="Группа удалена"
        onConfirm={async () => {
          await core.del(`/v1/admin/groups/${g.id}`)
          await refresh()
          navigate("/groups", { replace: true })
        }}
      />

      <ActionDialog
        open={dialog?.kind === "addNode"}
        onOpenChange={close}
        title={`Добавить ноду в «${g.name}»`}
        description="Нода бывает только в одной группе — выбранная перенесётся сюда"
        consequences={consequences}
        consequencesBelow
        destructive={isHarsh(lines)}
        confirmLabel="Перенести"
        canConfirm={!!pickedNode}
        success="Нода перенесена"
        onConfirm={async () => {
          if (!pickedNode) return
          await core.patch(`/v1/admin/nodes/${pickedNode.id}`, { group_id: gid })
          await refresh()
        }}
      >
        {allNodes.filter((n) => n.group_id !== gid).length === 0 ? (
          <Empty>Все ноды уже в этой группе</Empty>
        ) : (
          <Picker
            label="Нода"
            items={allNodes.filter((n) => n.group_id !== gid)}
            value={pick}
            onChange={setPick}
            render={(n) => (
              <>
                <span className="min-w-0 truncate font-medium">
                  {n.name}
                  {hiddenReason(n) && <span className="font-normal text-muted-foreground"> · {hiddenReason(n)}</span>}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">{where(n.group_id) ? `в «${where(n.group_id)}»` : "общая"}</span>
              </>
            )}
          />
        )}
      </ActionDialog>

      <ActionDialog
        open={dialog?.kind === "addPlan"}
        onOpenChange={close}
        title={`Добавить тариф в «${g.name}»`}
        description="Тариф бывает только в одной группе — выбранный перенесётся сюда"
        consequences={consequences}
        consequencesBelow
        destructive={isHarsh(lines)}
        confirmLabel="Перенести"
        canConfirm={!!pickedPlan}
        success="Тариф перенесён"
        onConfirm={async () => {
          if (!pickedPlan) return
          await core.patch(`/v1/admin/plans/${pickedPlan.id}`, { location_group_id: gid })
          await refresh()
        }}
      >
        {allPlans.filter((p) => p.location_group_id !== gid).length === 0 ? (
          <Empty>Все тарифы уже в этой группе</Empty>
        ) : (
          <Picker
            label="Тариф"
            items={allPlans.filter((p) => p.location_group_id !== gid)}
            value={pick}
            onChange={setPick}
            render={(p) => (
              <>
                <span className="min-w-0 truncate font-medium">
                  {p.name}
                  <span className="font-normal text-muted-foreground tabular-nums"> · {p.active_users} {plural(p.active_users, "юзер", "юзера", "юзеров")}</span>
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">{where(p.location_group_id) ? `в «${where(p.location_group_id)}»` : "без группы"}</span>
              </>
            )}
          />
        )}
      </ActionDialog>

      <ActionDialog
        open={dialog?.kind === "removeNode"}
        onOpenChange={close}
        title={dialog?.kind === "removeNode" ? `Убрать «${dialog.node.name}» из группы` : "Убрать ноду"}
        description="Нода останется без группы и станет общей"
        consequences={consequences}
        destructive={isHarsh(lines)}
        confirmLabel="Убрать"
        success="Нода теперь общая"
        onConfirm={async () => {
          if (dialog?.kind !== "removeNode") return
          await core.patch(`/v1/admin/nodes/${dialog.node.id}`, { group_id: null })
          await refresh()
        }}
      />

      <ActionDialog
        open={dialog?.kind === "removePlan"}
        onOpenChange={close}
        title={dialog?.kind === "removePlan" ? `Убрать «${dialog.plan.name}» из группы` : "Убрать тариф"}
        description="Тариф останется без группы"
        consequences={consequences}
        destructive={isHarsh(lines)}
        confirmLabel="Убрать"
        success="Тариф убран из группы"
        onConfirm={async () => {
          if (dialog?.kind !== "removePlan") return
          await core.patch(`/v1/admin/plans/${dialog.plan.id}`, { location_group_id: null })
          await refresh()
        }}
      />
    </>
  )
}
