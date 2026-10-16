import type { ReactNode } from "react"
import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"

export function Field({ id, label, hint, error, children, className }: { id?: string; label: ReactNode; hint?: ReactNode; error?: string | null; children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-2", className)}>
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error ? <p className="text-xs text-[oklch(0.82_0.12_22)]">{error}</p> : hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

export function splitList(s: string): string[] {
  return s
    .split(/[\n,]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

export function CheckChip({ checked, onChange, children, disabled }: { checked: boolean; onChange: (v: boolean) => void; children: ReactNode; disabled?: boolean }) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        "inline-flex h-8 items-center gap-1.5 rounded-full px-3 text-sm transition-[background-color,color,box-shadow,scale] duration-200 active:scale-[0.96] disabled:pointer-events-none disabled:opacity-50",
        checked ? "bg-primary/20 text-foreground shadow-[inset_0_0_0_1px_oklch(0.74_0.15_287/55%)]" : "glass-pill text-muted-foreground hover:bg-white/16 hover:text-foreground",
      )}
    >
      {children}
    </button>
  )
}
