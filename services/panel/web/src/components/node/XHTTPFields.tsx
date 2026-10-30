import { ChevronDown } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { alpnOptions, extraGroups, xhttpModes, type ExtraField, type ExtraForm } from "@/lib/inbounds"
import { cn } from "@/lib/utils"

const byDefault = "__default"

export function Field({ id, label, hint, children }: { id?: string; label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function ModeSelect({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger id={id}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {xhttpModes.map((m) => (
          <SelectItem key={m} value={m}>
            {m}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function ExtraInput({ field, value, onChange }: { field: ExtraField; value: string | boolean; onChange: (v: string | boolean) => void }) {
  const id = `extra-${field.key}`
  if (field.kind === "bool") {
    return (
      <div className="well flex items-center justify-between gap-3 rounded-xl p-3 sm:col-span-2">
        <div className="min-w-0">
          <Label htmlFor={id} className="text-sm font-medium">
            {field.label}
          </Label>
          {field.hint && <div className="text-xs text-muted-foreground">{field.hint}</div>}
        </div>
        <Switch id={id} checked={value === true} onCheckedChange={onChange} />
      </div>
    )
  }
  if (field.kind === "select") {
    return (
      <Field id={id} label={field.label} hint={field.hint}>
        <Select value={value === "" ? byDefault : String(value)} onValueChange={(v) => onChange(v === byDefault ? "" : v)}>
          <SelectTrigger id={id}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={byDefault}>как в xray ({field.placeholder})</SelectItem>
            {field.options.map((o) => (
              <SelectItem key={o} value={o}>
                {o}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
    )
  }
  if (field.kind === "headers") {
    return (
      <div className="sm:col-span-2">
        <Field id={id} label={field.label} hint={field.hint}>
          <Textarea id={id} value={String(value)} onChange={(e) => onChange(e.target.value)} rows={2} className="font-mono" placeholder="X-Requested-With: XMLHttpRequest" />
        </Field>
      </div>
    )
  }
  return (
    <Field id={id} label={field.label} hint={field.hint}>
      <Input
        id={id}
        value={String(value)}
        onChange={(e) => onChange(e.target.value)}
        placeholder={field.placeholder}
        inputMode={field.kind === "text" ? undefined : "numeric"}
        className="font-mono"
      />
    </Field>
  )
}

export function ExtraFields({ form, onChange }: { form: ExtraForm; onChange: (f: ExtraForm) => void }) {
  return (
    <div className="flex flex-col gap-2">
      {extraGroups.map((g) => {
        const set = g.fields.filter((f) => form[f.key] !== "" && form[f.key] !== false).length
        return (
          <details key={g.title} className="well group rounded-xl" open={set > 0 ? true : undefined}>
            <summary className="flex cursor-pointer list-none items-center gap-2 p-3 text-sm font-medium [&::-webkit-details-marker]:hidden">
              <ChevronDown className="size-4 shrink-0 -rotate-90 text-muted-foreground transition group-open:rotate-0" />
              <span className="min-w-0 flex-1">
                {g.title}
                {g.hint && <span className="block text-xs font-normal text-muted-foreground">{g.hint}</span>}
              </span>
              {set > 0 && <span className="text-xs text-muted-foreground">задано: {set}</span>}
            </summary>
            <div className="grid gap-4 px-3 pb-3 sm:grid-cols-2">
              {g.fields.map((f) => (
                <ExtraInput key={f.key} field={f} value={form[f.key] ?? ""} onChange={(v) => onChange({ ...form, [f.key]: v })} />
              ))}
            </div>
          </details>
        )
      })}
    </div>
  )
}

export function ALPNPicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  return (
    <div className="flex flex-wrap gap-2">
      {alpnOptions.map((a) => {
        const on = value.includes(a)
        return (
          <button
            key={a}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== a) : alpnOptions.filter((x) => x === a || value.includes(x)))}
            className={cn(
              "rounded-full border px-3 py-1 font-mono text-xs transition",
              on ? "border-primary/60 bg-primary/20 text-foreground" : "border-white/12 text-muted-foreground hover:text-foreground",
            )}
          >
            {a}
          </button>
        )
      })}
    </div>
  )
}
