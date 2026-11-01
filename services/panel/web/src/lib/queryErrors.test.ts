import { afterEach, describe, expect, it, vi } from "vitest"
import { ApiError } from "./errors"
import { createQueryErrorReporter, type ErrorToast } from "./queryErrors"

function setup() {
  vi.spyOn(console, "error").mockImplementation(() => {})
  const toasts: ErrorToast[] = []
  return { toasts, r: createQueryErrorReporter((t) => toasts.push(t)) }
}

const q = (hash: string) => ({ queryHash: hash, queryKey: [hash] })
const down = (rid: string) => new ApiError(502, "core_unavailable", "", "", rid)

describe("query error reporter", () => {
  afterEach(() => vi.restoreAllMocks())

  it("raises one toast per failure streak of a query, even while it keeps polling", () => {
    const { toasts, r } = setup()
    r.onError(down("rid-a"), q("overview"))
    r.onError(down("rid-b"), q("overview"))
    r.onError(down("rid-c"), q("overview"))
    expect(toasts).toHaveLength(1)
    expect(toasts[0].message).toBe("Не удалось загрузить данные: Ядро не отвечает. Код запроса: rid-a")
    r.onSuccess(q("overview"))
    r.onError(down("rid-d"), q("overview"))
    expect(toasts).toHaveLength(2)
    expect(console.error).toHaveBeenCalledTimes(4)
  })

  it("groups the same failure of different queries under one toast id", () => {
    const { toasts, r } = setup()
    r.onError(down("rid-a"), q("plans"))
    r.onError(down("rid-b"), q("nodes"))
    expect(toasts).toHaveLength(2)
    expect(toasts[0].id).toBe(toasts[1].id)
  })

  it("leaves expected client errors to the page", () => {
    const { toasts, r } = setup()
    r.onError(new ApiError(404, "not_found", "", ""), q("user"))
    r.onError(new ApiError(401, "unauthorized", "", ""), q("users"))
    r.onError(new ApiError(400, "bad_request", "", "cascade to itself"), q("preview"))
    expect(toasts).toHaveLength(0)
    expect(console.error).toHaveBeenCalledTimes(3)
  })

  it("is loud about network failures, broken replies and non-API errors", () => {
    const { toasts, r } = setup()
    r.onError(new ApiError(0, "network", "", "Панель не отвечает."), q("a"))
    r.onError(new ApiError(200, "bad_reply", "", "HTTP 200"), q("b"))
    r.onError(new TypeError("x is undefined"), q("c"))
    expect(toasts).toHaveLength(3)
  })

  it("forgets a query removed from the cache", () => {
    const { toasts, r } = setup()
    r.onError(down("rid-a"), q("overview"))
    r.forget(q("overview"))
    r.onError(down("rid-b"), q("overview"))
    expect(toasts).toHaveLength(2)
  })
})
