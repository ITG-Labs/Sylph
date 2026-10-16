import { useState } from "react"
import { Link, useNavigate } from "react-router"
import { ChevronRight, Plus, TriangleAlert } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Empty } from "@/components/common/Section"
import { Field } from "@/components/routing/parts"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { core } from "@/lib/api"
import { fmtDateTime, plural } from "@/lib/format"
import { useInvalidate, useProfiles } from "@/lib/queries"
import { isPlanRule, kindLabels, starterDoc, type Profile } from "@/lib/routing"

function Summary({ p }: { p: Profile }) {
  const kinds = [...new Set(p.doc.outbounds.map((o) => kindLabels[o.kind].toLowerCase()))]
  const plan = p.doc.rules.filter(isPlanRule).length
  return (
    <span className="text-muted-foreground">
      {p.doc.outbounds.length} {plural(p.doc.outbounds.length, "выход", "выхода", "выходов")} ({kinds.join(", ")}) · {p.doc.rules.length} {plural(p.doc.rules.length, "правило", "правила", "правил")}
      {plan > 0 && `, из них по тарифу ${plan}`}
    </span>
  )
}

export function RoutingPage() {
  const profiles = useProfiles()
  const invalidate = useInvalidate()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState("")

  return (
    <>
      <PageHeader
        title="Маршрутизация"
        description="профиль решает, куда уходит трафик юзеров ноды: напрямую, в блок, через прокси или другую ноду"
        actions={
          <Button
            onClick={() => {
              setName("")
              setCreating(true)
            }}
          >
            <Plus />
            Новый профиль
          </Button>
        }
      />
      {profiles.isPending ? (
        <LoadingBlock rows={3} />
      ) : profiles.isError ? (
        <LoadError error={profiles.error} onRetry={() => profiles.refetch()} />
      ) : profiles.data.length === 0 ? (
        <Empty>Профилей нет — ноды выпускают трафик напрямую</Empty>
      ) : (
        <>
          <div className="glass-thin hidden overflow-hidden rounded-2xl md:block">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Профиль</TableHead>
                  <TableHead>Что внутри</TableHead>
                  <TableHead className="text-right">Ноды</TableHead>
                  <TableHead>Изменён</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {profiles.data.map((p) => (
                  <TableRow key={p.id} className="cursor-pointer" onClick={() => navigate(`/routing/${p.id}`)}>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium">{p.name}</span>
                        {(p.warnings?.length ?? 0) > 0 && (
                          <Badge variant="warning">
                            <TriangleAlert />
                            {p.warnings!.length} {plural(p.warnings!.length, "предупреждение", "предупреждения", "предупреждений")}
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-normal">
                      <Summary p={p} />
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{p.nodes}</TableCell>
                    <TableCell className="text-muted-foreground">{fmtDateTime(p.updated_at)}</TableCell>
                    <TableCell className="w-8">
                      <ChevronRight className="size-4 text-muted-foreground" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="flex flex-col gap-2 md:hidden">
            {profiles.data.map((p) => (
              <Link key={p.id} to={`/routing/${p.id}`} className="glass-thin flex flex-col gap-2 rounded-2xl p-4 transition-transform duration-200 ease-glide active:scale-[0.985]">
                <div className="flex items-start justify-between gap-2">
                  <span className="font-medium">{p.name}</span>
                  <span className="text-sm text-muted-foreground tabular-nums">
                    {p.nodes} {plural(p.nodes, "нода", "ноды", "нод")}
                  </span>
                </div>
                {(p.warnings?.length ?? 0) > 0 && (
                  <Badge variant="warning" className="w-fit">
                    <TriangleAlert />
                    {p.warnings!.length} {plural(p.warnings!.length, "предупреждение", "предупреждения", "предупреждений")}
                  </Badge>
                )}
                <div className="text-xs">
                  <Summary p={p} />
                </div>
              </Link>
            ))}
          </div>
        </>
      )}
      <ActionDialog
        open={creating}
        onOpenChange={setCreating}
        title="Новый профиль"
        description="начнётся с выходов «напрямую» и «блок»; нодам его назначают на их странице"
        confirmLabel="Создать"
        canConfirm={name.trim() !== ""}
        success="Профиль создан"
        onConfirm={async () => {
          const p = await core.post<Profile>("/v1/admin/profiles", { name: name.trim(), doc: starterDoc() })
          await invalidate(["profiles"])
          navigate(`/routing/${p.id}`)
        }}
      >
        <Field id="pf-name" label="Название">
          <Input id="pf-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Европа через Нидерланды" maxLength={64} autoFocus />
        </Field>
      </ActionDialog>
    </>
  )
}
