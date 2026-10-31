import { describe, expect, it } from "vitest"
import { ApiError, describeError } from "./errors"

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
