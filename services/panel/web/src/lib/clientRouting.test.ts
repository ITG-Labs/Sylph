import { describe, expect, it } from "vitest"
import { issuesFor, moveRule, putBody, reasonLabels, ruleKey, type ClientRule } from "./clientRouting"

const a: ClientRule = { name: "a", action: "direct", domain: ["a.ru"] }
const b: ClientRule = { name: "b", action: "block", port: "25" }
const c: ClientRule = { name: "c", action: "node", node_id: 3, ip: ["geoip:ru"] }

describe("moveRule", () => {
  it("moves a rule down", () => {
    expect(moveRule([a, b, c], 0, 2).map((r) => r.name)).toEqual(["b", "c", "a"])
  })
  it("ignores out-of-range moves", () => {
    expect(moveRule([a, b], 0, -1).map((r) => r.name)).toEqual(["a", "b"])
    expect(moveRule([a, b], 1, 2).map((r) => r.name)).toEqual(["a", "b"])
  })
})

describe("putBody", () => {
  it("drops empty values, stray node ids and empty fields", () => {
    const body = putBody([{ name: " p ", action: "proxy", node_id: 5, domain: ["a.ru", " ", ""], ip: [], port: " ", network: "" }], 4)
    expect(body).toEqual({ rules: [{ name: "p", action: "proxy", domain: ["a.ru"] }], revision: 4 })
  })
  it("keeps the node id for node rules", () => {
    expect(putBody([c], 0).rules[0]).toEqual(c)
  })
})

describe("reasonLabels", () => {
  it("covers every reason the core sends", () => {
    for (const r of ["port", "network", "keyword", "regexp", "category_format", "domain_format", "node_via_active"]) {
      expect(reasonLabels[r]).toBeTruthy()
    }
  })
})

describe("issuesFor", () => {
  it("matches saved rules by content", () => {
    const saved = { rules: [a, b], issues: [[], [{ client: "happ" as const, skip: true, reason: "port" }]], missing: [[], []] }
    expect(issuesFor(saved, b)).toEqual({ issues: saved.issues[1], missing: [], checked: true })
    expect(issuesFor(saved, { ...b, port: "26" }).checked).toBe(false)
    expect(ruleKey(a)).toBe(ruleKey({ ...a }))
  })
})
