import { describe, expect, it } from "vitest"
import { assignable, deletable, holderLabel, needsAddress, statusOf } from "./addresses"
import type { NodeAddress, UserAddress } from "./types"

const addr = (id: number, node: number, extra: Partial<NodeAddress> = {}): NodeAddress => ({
  id,
  node_id: node,
  node_name: `n${node}`,
  ip: `203.0.113.${id}`,
  status: "ok",
  error: null,
  first_seen_at: "2026-10-24T00:00:00Z",
  last_seen_at: "2026-10-24T00:00:00Z",
  stale: false,
  assignment: null,
  balancer_warning: false,
  no_direct_warning: false,
  ...extra,
})

const holder = { user_id: 9, telegram_id: null, telegram_username: null, telegram_name: null, source: "admin" as const, note: "", assigned_at: "2026-10-24T00:00:00Z" }

const mine = (node: number, effective = true): UserAddress => ({
  address_id: 100 + node,
  node_id: node,
  node_name: `n${node}`,
  ip: "198.51.100.1",
  status: "ok",
  source: "admin",
  note: "",
  assigned_at: "2026-10-24T00:00:00Z",
  effective,
})

describe("assignable", () => {
  it("offers only free, present addresses on nodes where the user has none", () => {
    const all = [addr(1, 1), addr(2, 1, { assignment: holder }), addr(3, 2, { status: "missing" }), addr(4, 2, { status: "error" }), addr(5, 3), addr(6, 4, { stale: true })]
    expect(assignable(all, []).map((a) => a.id)).toEqual([1, 4, 5])
    expect(assignable(all, [mine(1)]).map((a) => a.id)).toEqual([4, 5])
    expect(assignable(all, undefined).map((a) => a.id)).toEqual([1, 4, 5])
  })
})

describe("holderLabel", () => {
  it("prefers the Telegram username, then the name, then the id", () => {
    expect(holderLabel({ ...holder, telegram_username: "vasya", telegram_name: "Вася", telegram_id: 5 })).toBe("@vasya")
    expect(holderLabel({ ...holder, telegram_name: "Вася", telegram_id: 5 })).toBe("Вася")
    expect(holderLabel({ ...holder, telegram_id: 5 })).toBe("5")
    expect(holderLabel(holder)).toBe("юзер № 9")
  })
})

describe("needsAddress", () => {
  it("warns only when the plan promises an address and none works", () => {
    expect(needsAddress(true, [])).toBe(true)
    expect(needsAddress(true, [mine(1, false)])).toBe(true)
    expect(needsAddress(true, [mine(1)])).toBe(false)
    expect(needsAddress(false, [])).toBe(false)
    expect(needsAddress(undefined, undefined)).toBe(false)
  })
})

describe("deletable and statusOf", () => {
  it("lets the operator drop addresses that are gone or whose node is silent", () => {
    expect(deletable(addr(1, 1))).toBe(false)
    expect(deletable(addr(1, 1, { status: "missing" }))).toBe(true)
    expect(deletable(addr(1, 1, { stale: true }))).toBe(true)
    expect(statusOf(addr(1, 1, { stale: true })).label).toBe("нода молчит")
    expect(statusOf(addr(1, 1, { status: "error" })).label).toBe("ошибка")
  })
})
