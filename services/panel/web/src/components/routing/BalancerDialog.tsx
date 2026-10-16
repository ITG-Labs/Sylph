import { useEffect, useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { freeTag, strategies, tagProblem, type Balancer, type RoutingDoc, type Strategy } from "@/lib/routing"
import { CheckChip, Field } from "./parts"

export function BalancerDialog({ open, onOpenChange, doc, balancer, onSave }: { open: boolean; onOpenChange: (v: boolean) => void; doc: RoutingDoc; balancer?: Balancer; onSave: (b: Balancer) => void }) {
  const [tag, setTag] = useState("")
  const [strategy, setStrategy] = useState<Strategy>("leastPing")
  const [picked, setPicked] = useState<string[]>([])
  const [fallback, setFallback] = useState("none")

  useEffect(() => {
    if (!open) return
    setTag(balancer?.tag ?? freeTag(doc, "best"))
    setStrategy(balancer?.strategy ?? "leastPing")
    setPicked(balancer?.outbounds ?? [])
    setFallback(balancer?.fallback || "none")
  }, [open, balancer, doc])

  const tagErr = tagProblem(doc, tag.trim(), balancer?.tag)
  const outbounds = doc.outbounds.map((o) => o.tag)
  const prefix = picked.flatMap((t) => outbounds.filter((o) => o !== t && o.startsWith(t)).map((o) => `«${t}» — начало «${o}»: xray возьмёт оба, переименуй один`))
  const problem = picked.length < 2 ? "отметь хотя бы два выхода" : (prefix[0] ?? null)

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={balancer ? `Балансировщик ${balancer.tag}` : "Новый балансировщик"}
      description="раскидывает трафик по нескольким выходам; правила и «весь остальной трафик» могут слать в него, как в выход"
      consequencesBelow
      consequences={problem ? <p>{problem}</p> : undefined}
      confirmLabel={balancer ? "Готово" : "Добавить"}
      canConfirm={!tagErr && !problem}
      onConfirm={async () => onSave({ tag: tag.trim(), strategy, outbounds: picked, ...(fallback !== "none" ? { fallback } : {}) })}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field id="bl-tag" label="Тег" error={tag.trim() ? tagErr : null}>
          <Input id="bl-tag" value={tag} onChange={(e) => setTag(e.target.value)} />
        </Field>
        <Field label="Как выбирать" hint={strategies.find((s) => s.value === strategy)?.hint}>
          <Select value={strategy} onValueChange={(v) => setStrategy(v as Strategy)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {strategies.map((s) => (
                <SelectItem key={s.value} value={s.value}>
                  {s.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Field label="Выходы">
        <div className="flex flex-wrap gap-2">
          {outbounds.map((t) => (
            <CheckChip key={t} checked={picked.includes(t)} onChange={(v) => setPicked((p) => (v ? [...p, t] : p.filter((x) => x !== t)))}>
              {t}
            </CheckChip>
          ))}
        </div>
      </Field>
      <Field label="Запасной выход" hint={strategy === "leastPing" ? "если ни один выход не отвечает на проверку" : "куда слать, если выбранные выходы недоступны"}>
        <Select value={fallback} onValueChange={setFallback}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">без запасного</SelectItem>
            {outbounds.map((t) => (
              <SelectItem key={t} value={t}>
                {t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
    </ActionDialog>
  )
}
