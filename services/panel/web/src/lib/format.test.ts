import { describe, expect, it } from "vitest"
import { fmtBytes, fmtRub, fmtDate, fmtDateTime, fmtUptime, fmtRelative, parseGB, plural, trafficPercent } from "./format"

describe("fmtBytes", () => {
  it("uses binary units with Russian names", () => {
    expect(fmtBytes(0)).toBe("0 Б")
    expect(fmtBytes(1023)).toBe("1023 Б")
    expect(fmtBytes(1536)).toBe("1,5 КБ")
    expect(fmtBytes(5368709120)).toBe("5 ГБ")
    expect(fmtBytes(214748364800)).toBe("200 ГБ")
    expect(fmtBytes(21100757449)).toBe("19,7 ГБ")
    expect(fmtBytes(1125899906842624)).toBe("1 ПБ")
  })
  it("shows unlimited for null", () => {
    expect(fmtBytes(null)).toBe("∞")
  })
})

describe("fmtRub", () => {
  it("groups thousands", () => {
    expect(fmtRub(0)).toBe("0 ₽")
    expect(fmtRub(1990)).toBe("1 990 ₽")
    expect(fmtRub(125000)).toBe("125 000 ₽")
  })
})

describe("dates in Moscow time", () => {
  it("formats regardless of the browser zone", () => {
    expect(fmtDate("2026-10-13T21:30:00Z")).toBe("14.10.2026")
    expect(fmtDateTime("2026-10-13T21:30:00Z")).toBe("14.10.2026, 00:30")
    expect(fmtDate(null)).toBe("—")
  })
  it("relative", () => {
    const now = new Date("2026-10-13T12:00:00Z")
    expect(fmtRelative("2026-10-13T11:59:30Z", now)).toBe("только что")
    expect(fmtRelative("2026-10-13T11:55:00Z", now)).toBe("5 мин назад")
    expect(fmtRelative("2026-10-13T09:00:00Z", now)).toBe("3 ч назад")
    expect(fmtRelative("2026-10-10T12:00:00Z", now)).toBe("3 дн назад")
    expect(fmtRelative(null, now)).toBe("никогда")
  })
})

describe("fmtUptime", () => {
  it("compact", () => {
    expect(fmtUptime(null)).toBe("—")
    expect(fmtUptime(45)).toBe("45 с")
    expect(fmtUptime(3600)).toBe("1 ч")
    expect(fmtUptime(3660)).toBe("1 ч 1 мин")
    expect(fmtUptime(86400 * 3 + 7200)).toBe("3 дн 2 ч")
  })
})

describe("parseGB", () => {
  it("accepts comma and dot decimals", () => {
    expect(parseGB("10")).toBe(10737418240)
    expect(parseGB("1,5")).toBe(1610612736)
    expect(parseGB(" 0.5 ")).toBe(536870912)
  })
  it("rejects junk", () => {
    expect(parseGB("")).toBeNull()
    expect(parseGB("abc")).toBeNull()
    expect(parseGB("-1")).toBeNull()
    expect(parseGB("0")).toBeNull()
  })
})

describe("trafficPercent", () => {
  it("clamps and handles unlimited", () => {
    expect(trafficPercent(50, 100)).toBe(50)
    expect(trafficPercent(150, 100)).toBe(100)
    expect(trafficPercent(10, null)).toBeNull()
    expect(trafficPercent(0, 0)).toBe(100)
  })
})

describe("plural", () => {
  it("follows Russian rules", () => {
    const f = (n: number) => plural(n, "нода", "ноды", "нод")
    expect([1, 2, 5, 11, 12, 14, 21, 22, 25, 101, 111].map(f)).toEqual(["нода", "ноды", "нод", "нод", "нод", "нод", "нода", "ноды", "нод", "нода", "нод"])
  })
})
