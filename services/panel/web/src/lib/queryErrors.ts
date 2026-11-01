import { ApiError, describeError, errorSummary } from "./errors"

export type ErrorToast = { id: string; message: string }

export function loudFailure(err: unknown): boolean {
  if (!(err instanceof ApiError)) return true
  return err.status === 0 || err.status >= 500 || err.code === "bad_reply"
}

export function createQueryErrorReporter(notify: (t: ErrorToast) => void) {
  const failing = new Set<string>()
  return {
    onError(err: unknown, query: { queryHash: string; queryKey: readonly unknown[] }) {
      console.error(`query ${JSON.stringify(query.queryKey)} failed`, err)
      if (!loudFailure(err) || failing.has(query.queryHash)) return
      failing.add(query.queryHash)
      notify({ id: `query-error:${errorSummary(err)}`, message: `Не удалось загрузить данные: ${describeError(err)}` })
    },
    onSuccess(query: { queryHash: string }) {
      failing.delete(query.queryHash)
    },
    forget(query: { queryHash: string }) {
      failing.delete(query.queryHash)
    },
  }
}
