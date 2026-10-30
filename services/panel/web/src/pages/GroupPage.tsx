import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { ArrowLeft, ListChecks, Pencil, Plus, Trash2, X } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { CheckList, type CheckItem } from "@/components/common/CheckList"
import { NodeStatusBadge } from "@/components/common/NodeStatusBadge"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty, Section } from "@/components/common/Section"
import { MoveConsequences } from "@/components/group/MoveConsequences"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { core } from "@/lib/api"
import { fmtDate, fmtRub, plural } from "@/lib/format"
import { accessChanges, describeChanges, hiddenReason, isHarsh, withMembers, withPlanGroup, type ImpactLine } from "@/lib/groups"
import { useInvalidate, useNodes } from "@/lib/queries"
import type { Plan } from "@/lib/types"
import { useWorld } from "@/lib/world"

type Dialog = { kind: "rename" } | { kind: "delete" } | { kind: "members" } | { kind: "addPlan" } | { kind: "removePlan"; plan: Plan }

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
  const { world, groups, plans, error, retry } = useWorld()
  const nodes = useNodes()
  const invalidate = useInvalidate()
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [pick, setPick] = useState<number | null>(null)
  const [name, setName] = useState("")
  const [checked, setChecked] = useState<number[]>([])

  if (error) return <LoadError error={error} onRetry={retry} />
  if (!world || !groups || !plans || !nodes.data) return <LoadingBlock rows={4} />

  const g = groups.find((x) => x.id === gid)
  if (!g) return <Empty>Группа не найдена</Empty>
  const nodeOf = (nid: number) => nodes.data.find((n) => n.id === nid)
  const inboundName = (iid: number) => {
    const i = world.inbounds.find((x) => x.id === iid)
    return i ? `${nodeOf(i.node_id)?.name ?? `нода ${i.node_id}`} · ${i.label || i.tag}` : `вход ${iid}`
  }
  const gPlans = plans.filter((p) => p.location_group_id === gid)
  const poolPlans = plans.filter((p) => p.pools.some((pool) => pool.group_id === gid))
  const memberCount = g.node_ids.length + g.inbound_ids.length
  const removable = memberCount === 0 && gPlans.length === 0 && poolPlans.length === 0
  const where = (group: number | null) => (group === null ? null : (groups.find((x) => x.id === group)?.name ?? `группа ${group}`))
  const refresh = () => invalidate(["nodes"], ["plans"], ["groups"], ["overview"])
  const open = (d: Dialog) => {
    setPick(null)
    setName(g.name)
    setChecked([...g.node_ids, ...g.inbound_ids.map((x) => -x)])
    setDialog(d)
  }
  const close = (v: boolean) => !v && setDialog(null)

  const pickedNodes = checked.filter((x) => x > 0)
  const pickedInbounds = checked.filter((x) => x < 0 && !pickedNodes.includes(world.inbounds.find((i) => i.id === -x)?.node_id ?? 0)).map((x) => -x)
  const tree: CheckItem[] = nodes.data.flatMap((n) => {
    const own = world.inbounds.filter((i) => i.node_id === n.id)
    const why = hiddenReason(n)
    const head: CheckItem = {
      id: n.id,
      title: (
        <>
          <span className="font-medium">{n.name}</span>
          {why && <span className="text-muted-foreground"> · {why}</span>}
        </>
      ),
      hint: "вся нода",
    }
    const whole = pickedNodes.includes(n.id)
    return [head, ...own.map((i) => ({ id: -i.id, title: i.label || i.tag, hint: whole ? "входит с нодой" : "только этот вход", indent: true, disabled: whole }))]
  })
  const sameMembers =
    pickedNodes.length === g.node_ids.length &&
    pickedNodes.every((x) => g.node_ids.includes(x)) &&
    pickedInbounds.length === g.inbound_ids.length &&
    pickedInbounds.every((x) => g.inbound_ids.includes(x))

  const pickedPlan = dialog?.kind === "addPlan" ? world.plans.find((p) => p.id === pick) : undefined
  let lines: ImpactLine[] = []
  if (dialog?.kind === "members" && !sameMembers) lines = describeChanges(world, accessChanges(world, withMembers(world, gid, pickedNodes, pickedInbounds)))
  if (pickedPlan) lines = describeChanges(world, accessChanges(world, withPlanGroup(world, pickedPlan.id, gid)))
  if (dialog?.kind === "removePlan") lines = describeChanges(world, accessChanges(world, withPlanGroup(world, dialog.plan.id, null)))
  const consequences = lines.length > 0 ? <MoveConsequences lines={lines} /> : undefined

  return (
    <>
      <Link to="/groups" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Группы
      </Link>
      <PageHeader
        title={g.name}
        description={`создана ${fmtDate(g.created_at)} · тарифы группы видят её состав и всё, что не входит ни в одну группу`}
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
          title="Состав"
          description="ноды целиком и отдельные входы; видны только тарифам этой группы и других групп, где они есть"
          action={
            <Button size="sm" onClick={() => open({ kind: "members" })}>
              <ListChecks />
              Изменить
            </Button>
          }
        >
          {memberCount === 0 ? (
            <Empty>Пусто — тарифы группы видят только то, что не входит ни в одну группу</Empty>
          ) : (
            <div className="flex flex-col gap-2">
              {g.node_ids.map((nid) => {
                const n = nodeOf(nid)
                return (
                  <div key={`n${nid}`} className="well flex items-center gap-3 rounded-xl py-2 pr-3 pl-3.5">
                    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2.5 gap-y-1">
                      <Link to={`/nodes/${nid}`} className="truncate font-medium hover:underline">
                        {n?.name ?? `нода ${nid}`}
                      </Link>
                      {n && <NodeStatusBadge status={n.status} />}
                    </div>
                    <span className="shrink-0 text-xs text-muted-foreground">вся нода</span>
                  </div>
                )
              })}
              {g.inbound_ids.map((iid) => (
                <div key={`i${iid}`} className="well flex items-center gap-3 rounded-xl py-2 pr-3 pl-3.5">
                  <span className="min-w-0 flex-1 truncate font-medium">{inboundName(iid)}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">вход</span>
                </div>
              ))}
            </div>
          )}
        </Section>

        <Section
          title="Тарифы"
          description="их юзеры видят состав группы и всё, что не входит ни в одну группу"
          action={
            <Button size="sm" onClick={() => open({ kind: "addPlan" })}>
              <Plus />
              Перенести сюда
            </Button>
          }
        >
          {gPlans.length === 0 && poolPlans.length === 0 ? (
            <Empty>Тарифов нет — состав группы сейчас никому не виден</Empty>
          ) : (
            <div className="flex flex-col gap-2">
              {gPlans.map((p) => (
                <div key={p.id} className="well flex items-center gap-3 rounded-xl py-2 pr-2 pl-3.5">
                  <div className="min-w-0 flex-1">
                    <Link to={`/plans/${p.id}`} className="block truncate font-medium hover:underline">
                      {p.name}
                    </Link>
                    <div className="text-xs text-muted-foreground tabular-nums">
                      {p.active_users ?? 0} {plural(p.active_users ?? 0, "юзер", "юзера", "юзеров")} сейчас · {fmtRub(p.price_rub)}
                    </div>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => open({ kind: "removePlan", plan: p })}>
                    <X />
                    Убрать
                  </Button>
                </div>
              ))}
              {poolPlans.map((p) => (
                <Link key={`pool${p.id}`} to={`/plans/${p.id}`} className="well block rounded-xl py-2 pr-3 pl-3.5 transition-colors hover:bg-white/6">
                  <div className="truncate font-medium">{p.name}</div>
                  <div className="text-xs text-muted-foreground">считает трафик группы отдельным пулом</div>
                </Link>
              ))}
            </div>
          )}
        </Section>
      </div>

      <ActionDialog
        open={dialog?.kind === "rename"}
        onOpenChange={close}
        title="Переименовать группу"
        description="Имя группы видно в панели и как подпись пула в боте, если у пула нет своего имени"
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
        title={removable ? `Удалить группу «${g.name}»` : "Группу пока не удалить"}
        consequences={
          removable ? (
            <p>Группа пустая и нигде не используется — юзеров это не коснётся.</p>
          ) : (
            <>
              <p>Удалить можно только неиспользуемую группу. Сначала:</p>
              {memberCount > 0 && <p>очисти состав;</p>}
              {gPlans.length > 0 && <p>перенеси в другие группы тарифы {quoted(gPlans.map((p) => p.name))};</p>}
              {poolPlans.length > 0 && <p>убери пулы с этой группой из тарифов {quoted(poolPlans.map((p) => p.name))}.</p>}
            </>
          )
        }
        destructive
        confirmLabel="Удалить"
        canConfirm={removable}
        success="Группа удалена"
        onConfirm={async () => {
          await core.del(`/v1/admin/groups/${g.id}`)
          await refresh()
          navigate("/groups", { replace: true })
        }}
      />

      <ActionDialog
        open={dialog?.kind === "members"}
        onOpenChange={close}
        title={`Состав «${g.name}»`}
        description="Отметь ноду целиком или только нужные входы. Нода или вход могут быть в нескольких группах сразу"
        consequences={consequences}
        consequencesBelow
        destructive={isHarsh(lines)}
        confirmLabel="Сохранить"
        canConfirm={!sameMembers}
        success="Состав сохранён"
        onConfirm={async () => {
          await core.put(`/v1/admin/groups/${g.id}/members`, { node_ids: pickedNodes, inbound_ids: pickedInbounds })
          await refresh()
        }}
      >
        {nodes.data.length === 0 ? <Empty>Нод пока нет</Empty> : <CheckList label="Состав" items={tree} value={checked} onChange={setChecked} />}
      </ActionDialog>

      <ActionDialog
        open={dialog?.kind === "addPlan"}
        onOpenChange={close}
        title={`Перенести тариф в «${g.name}»`}
        description="У тарифа одна группа или ни одной — выбранный перенесётся сюда"
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
        {world.plans.filter((p) => p.location_group_id !== gid).length === 0 ? (
          <Empty>Все тарифы уже в этой группе</Empty>
        ) : (
          <Picker
            label="Тариф"
            items={world.plans.filter((p) => p.location_group_id !== gid)}
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
        open={dialog?.kind === "removePlan"}
        onOpenChange={close}
        title={dialog?.kind === "removePlan" ? `Убрать «${dialog.plan.name}» из группы` : "Убрать тариф"}
        description="Тариф останется без группы и будет видеть только то, что не входит ни в одну группу"
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
