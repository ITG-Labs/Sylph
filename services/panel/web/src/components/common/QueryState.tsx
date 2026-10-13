import type { ReactNode } from "react"
import { RefreshCw, ServerCrash } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { describeError } from "@/lib/errors"

export function LoadError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 py-10 text-center">
      <ServerCrash className="size-7 text-destructive" />
      <div className="text-sm">{describeError(error)}</div>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCw />
          Повторить
        </Button>
      )}
    </div>
  )
}

export function LoadingBlock({ rows = 3 }: { rows?: number }): ReactNode {
  return (
    <div className="flex flex-col gap-3">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-16 w-full" />
      ))}
    </div>
  )
}
