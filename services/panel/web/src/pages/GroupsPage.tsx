import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
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
import { useGroups, useInvalidate, usePlans } from "@/lib/queries"
import type { Node } from "@/lib/types"

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
  const groups = useGroups()
  const plans = usePlans()
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: () => core.get<Node[]>("/v1/admin/nodes") })
  const invalidate = useInvalidate()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const rows = [
    { id: null as number | null, name: "Общие ноды", created_at: null as string | null },
    ...(groups.data ?? []).map((g) => ({ id: g.id as number | null, name: g.name, created_at: g.created_at as string | null })),
  ]

  return (
    <>
      <PageHeader
        title="Группы локаций"
        description="тариф видит ноды своей группы и общие ноды без группы"
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
      {groups.isPending ? (
        <LoadingBlock rows={2} />
      ) : groups.isError ? (
        <LoadError error={groups.error} onRetry={() => groups.refetch()} />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((g) => {
            const gn = nodes.data?.filter((n) => n.group_id === g.id) ?? []
            const gp = plans.data?.filter((p) => p.location_group_id === g.id) ?? []
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
                  <div className="text-xs text-muted-foreground">ноды</div>
                  {gn.length === 0 ? <span className="text-muted-foreground">нет</span> : gn.map((n) => n.name).join(", ")}
                </div>
                <div className="well rounded-xl px-3 py-2.5 text-sm">
                  <div className="text-xs text-muted-foreground">{g.id === null ? "тарифы без группы" : "тарифы"}</div>
                  {gp.length === 0 ? <span className="text-muted-foreground">нет</span> : gp.map((p) => p.name).join(", ")}
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
        description="Ноды и тарифы добавляются с экрана группы; удалить можно только пустую группу"
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
