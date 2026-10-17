import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Activity, CircleCheck, CircleX, ShieldCheck, Trash2 } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { core } from "@/lib/api"
import { fmtDate, fmtRelative, fmtUptime } from "@/lib/format"
import { certState, serviceTitle } from "@/lib/services"
import type { CoreInfo, ServiceInfo } from "@/lib/types"

const certTone = {
  none: "text-muted-foreground",
  ok: "text-muted-foreground",
  soon: "text-warning",
  urgent: "text-destructive",
  expired: "text-destructive",
} as const

function CertLine({ label, notAfter }: { label: string; notAfter: string | null }) {
  const st = certState(notAfter)
  const left = st.tone === "expired" ? "истёк" : st.days !== null && st.tone !== "ok" ? `через ${st.days} дн` : null
  return (
    <span className={certTone[st.tone]}>
      {label} {fmtDate(notAfter)}
      {left && <> · {left}</>}
    </span>
  )
}

export function ServicesCard({ services, coreInfo }: { services: ServiceInfo[]; coreInfo: CoreInfo }) {
  const qc = useQueryClient()
  const [forget, setForget] = useState<ServiceInfo | null>(null)

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Сервисы</CardTitle>
        <CardDescription>пульс раз в минуту · молчит дольше 3 минут — алерт</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col divide-y divide-hairline">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 py-3 text-sm">
          <div className="flex min-w-36 items-center gap-2 font-medium">
            <ShieldCheck className="size-4 text-muted-foreground" />
            Ядро
          </div>
          <span className="text-muted-foreground tabular-nums">{coreInfo.version || "—"}</span>
          <CertLine label="серт до" notAfter={coreInfo.cert_not_after} />
          <CertLine label="CA до" notAfter={coreInfo.ca_not_after} />
        </div>
        {services.length === 0 && <div className="py-4 text-sm text-muted-foreground">Сервисы ещё не присылали пульс</div>}
        {services.map((s) => (
          <div key={s.cert_cn} className="flex flex-wrap items-center gap-x-4 gap-y-1.5 py-3 text-sm">
            <div className="flex min-w-36 items-center gap-2 font-medium">
              <Activity className="size-4 text-muted-foreground" />
              {serviceTitle(s.role, s.cert_cn)}
            </div>
            {s.alive ? (
              <Badge variant="success">
                <CircleCheck />
                жив
              </Badge>
            ) : (
              <Badge variant="destructive">
                <CircleX />
                молчит
              </Badge>
            )}
            <span className="text-muted-foreground tabular-nums">{s.version || "—"}</span>
            <span className="text-muted-foreground">
              {s.alive ? `работает ${fmtUptime(s.uptime_sec)}` : `последний пульс ${fmtRelative(s.last_seen)}`}
            </span>
            <CertLine label="серт до" notAfter={s.cert_not_after} />
            {!s.alive && (
              <Button variant="ghost" size="sm" className="ml-auto" onClick={() => setForget(s)}>
                <Trash2 /> забыть
              </Button>
            )}
          </div>
        ))}
      </CardContent>
      <ActionDialog
        open={forget !== null}
        onOpenChange={(open) => !open && setForget(null)}
        title={forget ? `Забыть «${serviceTitle(forget.role, forget.cert_cn)}»?` : ""}
        consequences={
          <>
            <span>Сервис пропадёт из этого списка, и монитор перестанет присылать алерт, что он молчит.</span>
            <span>Если сервис снова пришлёт пульс, он появится сам.</span>
          </>
        }
        confirmLabel="Забыть"
        success="Сервис забыт"
        onConfirm={async () => {
          if (!forget) return
          await core.del(`/v1/admin/services/${encodeURIComponent(forget.cert_cn)}`)
          await qc.invalidateQueries({ queryKey: ["overview"] })
        }}
      />
    </Card>
  )
}
