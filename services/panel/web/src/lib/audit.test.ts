import { describe, expect, it } from "vitest"
import { auditAction, auditDetails, auditLink } from "./audit"

describe("audit", () => {
  it("humanizes known actions and falls back", () => {
    expect(auditAction("user", "ban")).toBe("забанил юзера")
    expect(auditAction("payment", "refund")).toBe("отметил возврат")
    expect(auditAction("payments", "recheck")).toBe("перепроверил платёж в ЮKassa")
    expect(auditAction("widget", "zap")).toBe("zap widget")
  })
  it("links entities", () => {
    expect(auditLink("users", "7")).toBe("/users/7")
    expect(auditLink("payment", "a/b")).toBe("/payments/a%2Fb")
    expect(auditLink("plan_access", "4")).toBe("/plans/4")
    expect(auditLink("inbound", "3")).toBeNull()
    expect(auditLink("payments", "")).toBeNull()
    expect(auditLink("payments", "p-1")).toBe("/payments/p-1")
  })
  it("flattens details", () => {
    expect(auditDetails({ days: 7, reason: "x" })).toBe("days: 7 · reason: x")
    expect(auditDetails({ reality: { dest: "a:443" } })).toBe('reality: {"dest":"a:443"}')
    expect(auditDetails(null)).toBe("")
  })
})
