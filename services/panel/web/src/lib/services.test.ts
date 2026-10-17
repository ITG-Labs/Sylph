import { describe, expect, it } from "vitest"
import { certState, serviceTitle } from "./services"

describe("certState", () => {
  const now = new Date("2026-10-17T09:00:00Z")
  const inDays = (d: number) => new Date(now.getTime() + d * 86400_000).toISOString()
  it("grades the time left like the monitor", () => {
    expect(certState(null, now)).toEqual({ tone: "none", days: null })
    expect(certState(inDays(200), now)).toEqual({ tone: "ok", days: 200 })
    expect(certState(inDays(20), now)).toEqual({ tone: "soon", days: 20 })
    expect(certState(inDays(5), now)).toEqual({ tone: "urgent", days: 5 })
    expect(certState(inDays(-1), now)).toEqual({ tone: "expired", days: -1 })
  })
})

describe("serviceTitle", () => {
  it("names roles in Russian and shows a non-default CN", () => {
    expect(serviceTitle("bot", "bot")).toBe("Бот")
    expect(serviceTitle("cron", "cron")).toBe("Крон")
    expect(serviceTitle("subs", "subs-2")).toBe("subs (subs-2)")
    expect(serviceTitle("monitor", "monitor")).toBe("Монитор")
  })
})
