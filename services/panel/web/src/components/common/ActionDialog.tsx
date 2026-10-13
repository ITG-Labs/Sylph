import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { AlertTriangle, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { describeError } from "@/lib/errors"

export interface ActionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: ReactNode
  consequences?: ReactNode
  confirmLabel: string
  destructive?: boolean
  reason?: { label: string; required?: boolean; placeholder?: string }
  canConfirm?: boolean
  children?: ReactNode
  success?: string
  onConfirm: (reason: string) => Promise<unknown>
}

export function ActionDialog({
  open,
  onOpenChange,
  title,
  description,
  consequences,
  confirmLabel,
  destructive,
  reason,
  canConfirm = true,
  children,
  success,
  onConfirm,
}: ActionDialogProps) {
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (open) {
      setText("")
      setError("")
      setBusy(false)
    }
  }, [open])

  const reasonMissing = !!reason?.required && text.trim() === ""

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy || reasonMissing || !canConfirm) return
    setBusy(true)
    setError("")
    try {
      await onConfirm(text.trim())
      if (success) toast.success(success)
      onOpenChange(false)
    } catch (err) {
      setError(describeError(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !busy && onOpenChange(v)}>
      <DialogContent>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            {description ? <DialogDescription>{description}</DialogDescription> : <DialogDescription className="sr-only">{title}</DialogDescription>}
          </DialogHeader>
          {consequences && (
            <div className={destructive ? "flex gap-3 tint-rim rounded-xl border border-destructive/30 bg-destructive/[0.08] p-3 text-sm" : "flex gap-3 tint-rim rounded-xl border border-warning/25 bg-warning/[0.07] p-3 text-sm"}>
              <AlertTriangle className={destructive ? "mt-0.5 size-4 shrink-0 text-destructive" : "mt-0.5 size-4 shrink-0 text-warning"} />
              <div className="flex flex-col gap-1.5 leading-relaxed">{consequences}</div>
            </div>
          )}
          {children}
          {reason && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="action-reason">
                {reason.label}
                {!reason.required && <span className="font-normal text-muted-foreground">(необязательно)</span>}
              </Label>
              <Textarea id="action-reason" maxLength={256} placeholder={reason.placeholder} value={text} onChange={(e) => setText(e.target.value)} />
            </div>
          )}
          {error && (
            <div role="alert" className="rounded-xl border border-destructive/30 bg-destructive/12 px-3.5 py-2.5 text-sm text-[oklch(0.82_0.12_22)]">
              {error}
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
              Отмена
            </Button>
            <Button type="submit" variant={destructive ? "destructive" : "default"} disabled={busy || reasonMissing || !canConfirm}>
              {busy && <Loader2 className="animate-spin" />}
              {confirmLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
