import { afterEach, describe, expect, it, vi } from "vitest"
import { ApiError } from "./errors"
import { queryErrorToast } from "./queryErrors"

describe("queryErrorToast", () => {
  afterEach(() => vi.restoreAllMocks())

  it("groups identical failures into one toast and keeps the request id in the text", () => {
    vi.spyOn(console, "error").mockImplementation(() => {})
    const a = queryErrorToast(new ApiError(502, "core_unavailable", "", "", "rid-a"), ["plans"])
    const b = queryErrorToast(new ApiError(502, "core_unavailable", "", "", "rid-b"), ["nodes"])
    expect(a.id).toBe(b.id)
    expect(a.message).toBe("Не удалось загрузить данные: Ядро не отвечает. Код запроса: rid-a")
    expect(console.error).toHaveBeenCalledTimes(2)
  })

  it("separates different failures", () => {
    vi.spyOn(console, "error").mockImplementation(() => {})
    const a = queryErrorToast(new ApiError(502, "core_unavailable", "", ""), ["plans"])
    const b = queryErrorToast(new ApiError(404, "not_found", "", ""), ["user", 1])
    expect(a.id).not.toBe(b.id)
  })
})
