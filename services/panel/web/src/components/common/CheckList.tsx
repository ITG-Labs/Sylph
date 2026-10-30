import { Check } from "lucide-react"

export interface CheckItem {
  id: number
  title: React.ReactNode
  hint?: React.ReactNode
  disabled?: boolean
  indent?: boolean
}

export function CheckList({ label, items, value, onChange }: { label: string; items: CheckItem[]; value: number[]; onChange: (ids: number[]) => void }) {
  const toggle = (id: number) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id])
  return (
    <div role="group" aria-label={label} className="well flex max-h-72 flex-col gap-0.5 overflow-y-auto rounded-xl p-1.5">
      {items.map((item) => {
        const on = value.includes(item.id)
        return (
          <button
            key={item.id}
            type="button"
            role="checkbox"
            aria-checked={on}
            disabled={item.disabled}
            onClick={() => toggle(item.id)}
            className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors duration-150 hover:bg-white/6 focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-45 aria-checked:bg-primary/16 ${item.indent ? "ml-6 w-[calc(100%-1.5rem)]" : ""}`}
          >
            <span
              className={`flex size-4 shrink-0 items-center justify-center rounded-[5px] border ${on ? "border-primary bg-primary text-primary-foreground" : "border-white/25"}`}
            >
              {on && <Check className="size-3" strokeWidth={3} />}
            </span>
            <span className="min-w-0 flex-1 truncate">{item.title}</span>
            {item.hint && <span className="shrink-0 text-xs text-muted-foreground">{item.hint}</span>}
          </button>
        )
      })}
    </div>
  )
}
