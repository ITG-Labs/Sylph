import { describe, expect, it } from "vitest"
import { ApiError, describeError, errorSummary } from "./errors"

describe("describeError", () => {
  it("explains known conflicts by field", () => {
    expect(describeError(new ApiError(409, "conflict", "user_id", "banned"))).toBe("Пользователь заблокирован. Сначала разблокируйте его.")
    expect(describeError(new ApiError(409, "conflict", "subscription", "x"))).toBe("У пользователя нет активного периода.")
    expect(describeError(new ApiError(409, "conflict", "bytes", "x"))).toBe("У выбранного пула нет лимита трафика.")
    expect(describeError(new ApiError(409, "conflict", "status", "x"))).toBe("Статус платежа уже изменился. Обновите страницу.")
    expect(describeError(new ApiError(409, "conflict", "status", "payment is still waiting for the customer"))).toBe("С момента создания платежа не прошло суток: пользователь ещё может его оплатить.")
  })
  it("explains codes", () => {
    expect(describeError(new ApiError(502, "provider_unavailable", "", ""))).toBe("ЮKassa не отвечает. Повторите попытку позже.")
    expect(describeError(new ApiError(502, "core_unavailable", "", ""))).toBe("Ядро не отвечает.")
    expect(describeError(new ApiError(404, "not_found", "", ""))).toBe("Не найдено.")
    expect(describeError(new ApiError(429, "too_many_attempts", "", ""))).toBe("Слишком много попыток входа. Повторите через 15 минут.")
  })
  it("falls back to the core message with the field", () => {
    expect(describeError(new ApiError(400, "bad_request", "reality.dest", "dest must not point to localhost"))).toBe("dest: dest must not point to localhost")
    expect(describeError(new Error("boom"))).toBe("boom")
  })
})

describe("request ids", () => {
  it("adds the request id to server failures only", () => {
    expect(describeError(new ApiError(500, "internal", "", "boom", "abc-123"))).toBe("Внутренняя ошибка ядра. Подробности — в журнале ядра. Код запроса: abc-123")
    expect(describeError(new ApiError(502, "core_unavailable", "", "", "abc-123"))).toBe("Ядро не отвечает. Код запроса: abc-123")
    expect(describeError(new ApiError(404, "not_found", "", "", "abc-123"))).toBe("Не найдено.")
    expect(describeError(new ApiError(500, "internal", "", "boom"))).toBe("Внутренняя ошибка ядра. Подробности — в журнале ядра.")
  })
  it("keeps the summary free of the request id", () => {
    expect(errorSummary(new ApiError(502, "core_unavailable", "", "", "abc-123"))).toBe("Ядро не отвечает.")
  })
  it("explains replies that are not JSON", () => {
    expect(describeError(new ApiError(200, "bad_reply", "", "HTTP 200", "abc-123"))).toBe("Ответ сервера не в формате JSON (HTTP 200). Код запроса: abc-123")
    expect(describeError(new ApiError(502, "bad_reply", "", "HTTP 502"))).toBe("Ответ сервера не в формате JSON (HTTP 502).")
  })
})

describe("cut replies", () => {
  it("keeps the request id when the reply broke off", () => {
    expect(describeError(new ApiError(0, "network", "", "Ответ сервера оборвался.", "abc-123"))).toBe("Ответ сервера оборвался. Код запроса: abc-123")
  })
})
