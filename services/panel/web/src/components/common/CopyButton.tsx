import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"

export function CopyButton({ value, label = "Скопировать" }: { value: string; label?: string }) {
  const [done, setDone] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setDone(true)
      setTimeout(() => setDone(false), 1500)
    } catch {
      toast.error("Браузер не дал скопировать")
    }
  }
  return (
    <Button type="button" variant="ghost" size="icon-sm" onClick={copy} title={label} aria-label={label}>
      {done ? <Check className="text-success" /> : <Copy />}
    </Button>
  )
}
