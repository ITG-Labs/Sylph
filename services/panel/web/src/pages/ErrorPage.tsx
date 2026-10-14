import { isRouteErrorResponse, Link, useRouteError } from "react-router"
import { Bug } from "lucide-react"
import { Button } from "@/components/ui/button"

export function ErrorPage() {
  const err = useRouteError()
  const text = isRouteErrorResponse(err) ? `${err.status} ${err.statusText}` : err instanceof Error ? err.message : String(err)
  return (
    <div className="glass-thin flex flex-col items-center gap-4 rounded-2xl px-6 py-14 text-center shadow-[inset_0_0_0_1px_oklch(0.66_0.21_22/30%)]">
      <Bug className="size-8 text-destructive" />
      <div>
        <div className="font-medium">Экран сломался</div>
        <div className="mt-1 max-w-xl text-sm break-words text-muted-foreground">{text}</div>
      </div>
      <div className="flex gap-2">
        <Button variant="outline" onClick={() => window.location.reload()}>
          Перезагрузить
        </Button>
        <Button variant="ghost" asChild>
          <Link to="/">На сводку</Link>
        </Button>
      </div>
    </div>
  )
}
