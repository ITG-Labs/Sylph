import { useState } from "react"
import { ChevronRight, Layers, Plus } from "lucide-react"
import { Link } from "react-router"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { core } from "@/lib/api"
import { fmtDate } from "@/lib/format"
import { groupsOf } from "@/lib/groups"
import { useInvalidate } from "@/lib/queries"
import { useWorld } from "@/lib/world"

function Card({ id, children }: { id: number | null; children: React.ReactNode }) {
  const cls = "glass-thin flex flex-col gap-3 rounded-2xl p-5"
  if (id === null) return <div className={cls}>{children}</div>
  return (
    <Link to={`/groups/${id}`} className={`${cls} transition-[background-color,scale] duration-200 ease-glide hover:bg-white/[0.07] active:scale-[0.985]`}>
      {children}
    </Link>
  )
}

export function GroupsPage() {
  const { world, groups, error, retry } = useWorld()
  const invalidate = useInvalidate()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const nodeName = (id: number) => world?.nodes.find((n) => n.id === id)?.name ?? `нода ${id}`
  const inboundName = (id: number) => {
    const i = world?.inbounds.find((x) => x.id === id)
    return i ? `${nodeName(i.node_id)} · ${i.label || i.tag}` : `вход ${id}`
  }
  const common = world ? world.nodes.filter((n) => world.inbounds.some((i) => i.node_id === n.id && groupsOf(world, i).length === 0)) : []
  const rows = [
    {
      id: null as number | null,
      name: "Общие ноды",
      created_at: null as string | null,
      members: common.map((n) => n.name),
      plans: (world?.plans ?? []).filter((p) => p.location_group_id === null).map((p) => p.name),
    },
    ...(groups ?? []).map((g) => ({
      id: g.id as number | null,
      name: g.name,
      created_at: g.created_at as string | null,
      members: [...g.node_ids.map(nodeName), ...g.inbound_ids.map(inboundName)],
      plans: (world?.plans ?? []).filter((p) => p.location_group_id === g.id).map((p) => p.name),
    })),
  ]

  return (
    <>
      <PageHeader
        title="Группы локаций"
        description="тариф видит состав своей группы и всё, что не входит ни в одну группу"
        actions={
          <Button
            onClick={() => {
              setName("")
              setOpen(true)
            }}
          >
            <Plus />
            Новая группа
          </Button>
        }
      />
      {error ? (
        <LoadError error={error} onRetry={retry} />
      ) : !world ? (
        <LoadingBlock rows={2} />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((g) => {
            return (
              <Card key={g.id ?? "common"} id={g.id}>
                <div className="flex items-center gap-2.5">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/15 text-primary shadow-[inset_0_1px_0_0_oklch(1_0_0/14%)]">
                    <Layers className="size-4" />
                  </span>
                  <span className="font-semibold">{g.name}</span>
                  {g.created_at && <span className="ml-auto text-xs text-muted-foreground">{fmtDate(g.created_at)}</span>}
                  {g.id !== null && <ChevronRight className="size-4 shrink-0 text-muted-foreground" />}
                </div>
                <div className="well rounded-xl px-3 py-2.5 text-sm">
                  <div className="text-xs text-muted-foreground">{g.id === null ? "ноды, где есть входы без группы" : "состав"}</div>
                  {g.members.length === 0 ? <span className="text-muted-foreground">нет</span> : g.members.join(", ")}
                </div>
                <div className="well rounded-xl px-3 py-2.5 text-sm">
                  <div className="text-xs text-muted-foreground">{g.id === null ? "тарифы без группы (общие ноды видят все)" : "тарифы"}</div>
                  {g.plans.length === 0 ? <span className="text-muted-foreground">нет</span> : g.plans.join(", ")}
                </div>
              </Card>
            )
          })}
        </div>
      )}
      <ActionDialog
        open={open}
        onOpenChange={setOpen}
        title="Новая группа"
        description="Ноды, входы и тарифы добавляются с экрана группы; удалить можно только неиспользуемую группу"
        confirmLabel="Создать"
        canConfirm={name.trim() !== ""}
        success="Группа создана"
        onConfirm={async () => {
          await core.post("/v1/admin/groups", { name: name.trim() })
          await invalidate(["groups"])
        }}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="gname">Название</Label>
          <Input id="gname" value={name} onChange={(e) => setName(e.target.value)} placeholder="Европа" autoFocus />
        </div>
      </ActionDialog>
    </>
  )
}
