import { useState } from "react"
import { Link, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"
import { ArrowLeft, Check, CheckCheck, RefreshCw, Undo2, XCircle } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { CopyButton } from "@/components/common/CopyButton"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Section } from "@/components/common/Section"
import { PaymentStatusBadge, SourceLabel, paymentStatusLabel } from "@/components/common/StatusBadges"
import { Button } from "@/components/ui/button"
import { core } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { fmtDateTime, fmtRub } from "@/lib/format"
import { useInvalidate } from "@/lib/queries"
import type { Payment, PaymentActionResult } from "@/lib/types"

type Action = "apply" | "reject" | "refund"

const providerStatus: Record<string, string> = {
  pending: "ждёт оплаты",
  waiting_for_capture: "ждёт подтверждения",
  succeeded: "оплачен",
  canceled: "отменён",
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[9rem_1fr] gap-3 border-b border-white/6 py-3 text-sm last:border-0 sm:grid-cols-[12rem_1fr]">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  )
}

export function PaymentPage() {
  const { paymentId = "" } = useParams()
  const invalidate = useInvalidate()
  const [action, setAction] = useState<Action | null>(null)
  const [recheck, setRecheck] = useState<PaymentActionResult | null>(null)
  const [rechecking, setRechecking] = useState(false)
  const [recheckError, setRecheckError] = useState("")
  const path = `/v1/admin/payments/${encodeURIComponent(paymentId)}`
  const q = useQuery({ queryKey: ["payment", paymentId], queryFn: () => core.get<Payment>(path) })

  if (q.isPending) return <LoadingBlock rows={4} />
  if (q.isError) return <LoadError error={q.error} onRetry={() => q.refetch()} />
  const p = q.data
  const refresh = () => invalidate(["payment", paymentId], ["payments"], ["overview"], ["user"], ["audit"])
  const stuckPending = p.status === "pending"

  const doRecheck = async () => {
    setRechecking(true)
    setRecheckError("")
    try {
      setRecheck(await core.post<PaymentActionResult>(`${path}/recheck`))
      await refresh()
    } catch (e) {
      setRecheckError(describeError(e))
    } finally {
      setRechecking(false)
    }
  }
  const act = (a: Action) => async (reason: string) => {
    const res = await core.post<PaymentActionResult>(`${path}/${a}`, { reason })
    if (a === "reject") toast.success(`Итог: ${paymentStatusLabel(res.status)}`)
    setRecheck(null)
    await refresh()
  }

  return (
    <>
      <Link to="/payments" className="glass-pill mb-4 inline-flex h-8 items-center gap-1.5 rounded-full pr-3.5 pl-2.5 text-sm text-muted-foreground transition-[background-color,color,scale] duration-200 hover:bg-white/16 hover:text-foreground active:scale-[0.96]">
        <ArrowLeft className="size-4" />
        Платежи
      </Link>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            {fmtRub(p.amount_rub)} · {p.plan_name}
            <PaymentStatusBadge status={p.status} />
          </span>
        }
        description={
          <span className="flex items-center gap-1 font-mono text-xs">
            {p.payment_id}
            <CopyButton value={p.payment_id} label="Скопировать id платежа" />
          </span>
        }
        actions={
          <>
            {p.source === "yookassa" && (
              <Button variant="secondary" onClick={doRecheck} disabled={rechecking}>
                <RefreshCw className={rechecking ? "animate-spin" : undefined} />
                Перепроверить в ЮKassa
              </Button>
            )}
            {(p.status === "mismatch" || p.status === "rejected") && (
              <Button onClick={() => setAction("apply")}>
                <CheckCheck />
                Провести
              </Button>
            )}
            {(p.status === "mismatch" || stuckPending) && (
              <Button variant="outline" onClick={() => setAction("reject")}>
                <XCircle />
                Отклонить
              </Button>
            )}
            {p.status === "succeeded" && (
              <Button variant="outline" onClick={() => setAction("refund")}>
                <Undo2 />
                Отметить возврат
              </Button>
            )}
          </>
        }
      />

      {(recheck || recheckError) && (
        <div className="mb-4">
          {recheckError ? (
            <div className="tint-rim rounded-2xl border border-destructive/30 bg-destructive/[0.08] p-4 text-sm text-destructive">{recheckError}</div>
          ) : recheck?.provider ? (
            <div className="tint-rim flex gap-3 rounded-2xl border border-info/25 bg-info/[0.07] p-4 text-sm">
              <Check className="mt-0.5 size-4 shrink-0 text-info" />
              {recheck.provider.found ? (
                <div className="flex flex-col gap-1">
                  <div className="font-medium">
                    ЮKassa: {providerStatus[recheck.provider.status ?? ""] ?? recheck.provider.status}, сумма {recheck.provider.amount} ₽
                    {recheck.provider.refunded && recheck.provider.refunded !== "0.00" ? `, возвращено ${recheck.provider.refunded} ₽` : ""}
                  </div>
                  <div className="text-muted-foreground">
                    {recheck.applied ? "Платёж проведён — юзер получил период." : `У нас: ${paymentStatusLabel(recheck.status)}.`}
                    {recheck.status === "succeeded" && recheck.provider.refunded && recheck.provider.refunded !== "0.00" && " Деньги вернули — отметь возврат, чтобы закрыть период."}
                  </div>
                </div>
              ) : (
                <div className="font-medium">ЮKassa такого платежа не знает</div>
              )}
            </div>
          ) : null}
        </div>
      )}

      <Section title="Подробности">
        <div className="flex flex-col">
          <Row label="Юзер">
            <Link to={`/users/${p.user_id}`} className="text-primary hover:underline">
              {p.telegram_id ?? "—"} · № {p.user_id}
            </Link>
          </Row>
          <Row label="Тариф">{p.plan_name}</Row>
          <Row label="Источник">
            <SourceLabel source={p.source} />
          </Row>
          <Row label="Создан">{fmtDateTime(p.created_at)}</Row>
          <Row label="Оплачен">{fmtDateTime(p.paid_at)}</Row>
          <Row label="Период">{p.period_id ? `№ ${p.period_id}` : "—"}</Row>
          {p.refunded_at && <Row label="Возврат отмечен">{fmtDateTime(p.refunded_at)}</Row>}
          {p.note && <Row label="Заметка">{p.note}</Row>}
        </div>
      </Section>

      <ActionDialog
        open={action === "apply"}
        onOpenChange={(v) => !v && setAction(null)}
        title="Провести всё равно"
        consequences={<p>Юзер получит период по обычным правилам оплаты: тот же тариф — в очередь, другой — сразу, остаток текущего сгорит.</p>}
        reason={{ label: "Причина", required: true, placeholder: "например: доплатил разницу переводом" }}
        confirmLabel="Провести"
        success="Платёж проведён"
        onConfirm={act("apply")}
      />
      <ActionDialog
        open={action === "reject"}
        onOpenChange={(v) => !v && setAction(null)}
        title="Отклонить платёж"
        consequences={
          stuckPending ? (
            <p>Ядро сначала спросит ЮKassa: если там окончательный статус — запишется он (оплачен — проведётся); если ЮKassa платёж не знает или он висит дольше суток — отклонится; моложе суток — откажет.</p>
          ) : (
            <p>Платёж станет отклонённым, период не выдаётся. Если деньги всё же пришли — его можно будет провести позже.</p>
          )
        }
        destructive
        reason={{ label: "Причина", required: true, placeholder: "например: сумма не сходится, деньги вернули" }}
        confirmLabel="Отклонить"
        onConfirm={act("reject")}
      />
      <ActionDialog
        open={action === "refund"}
        onOpenChange={(v) => !v && setAction(null)}
        title="Отметить возврат"
        consequences={
          <>
            <p className="font-medium">Панель деньги не двигает — верни их сам в кабинете ЮKassa{p.source === "manual" ? " или как платил юзер" : ""}.</p>
            <p>Платёж станет «возвращён», его период закроется, если ещё идёт или ждёт в очереди.</p>
          </>
        }
        destructive
        reason={{ label: "Причина", required: true, placeholder: "например: вернул в кабинете, юзер передумал" }}
        confirmLabel="Отметить возврат"
        success="Возврат отмечен"
        onConfirm={act("refund")}
      />
    </>
  )
}
