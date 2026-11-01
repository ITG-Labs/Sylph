import { describeError, errorSummary } from "./errors"

export type ErrorToast = { id: string; message: string }

export function queryErrorToast(err: unknown, queryKey: readonly unknown[]): ErrorToast {
  console.error(`query ${JSON.stringify(queryKey)} failed`, err)
  return { id: `query-error:${errorSummary(err)}`, message: `Не удалось загрузить данные: ${describeError(err)}` }
}
