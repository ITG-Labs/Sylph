import { describe, expect, it } from "vitest"
import { ApiError } from "./errors"
import {
  changedCascades,
  describeRoutingError,
  docProblems,
  freeTag,
  orderRules,
  renameTag,
  restartsXray,
  ruleChips,
  ruleTemplates,
  starterDoc,
  tagProblem,
  usages,
  type RoutingDoc,
} from "./routing"

function doc(): RoutingDoc {
  const d = starterDoc()
  d.outbounds.push({ tag: "nl", kind: "proxy", proxy: { protocol: "socks", address: "nl.example.com", port: 1080 } }, { tag: "fi", kind: "cascade", cascade: { node_id: 2 } })
  d.balancers.push({ tag: "best", strategy: "leastPing", outbounds: ["nl", "fi"], fallback: "direct" })
  d.rules.push({ name: "Торренты", protocol: ["bittorrent"], to: "block" }, { plans: [3], to: "nl" })
  d.default = "best"
  return d
}

describe("routing", () => {
  it("templates point at the first block and direct outbounds", () => {
    const d = doc()
    const all = Object.fromEntries(ruleTemplates.map((t) => [t.id, t.rules(d)]))
    expect(all.torrents).toEqual([{ name: "Торренты", protocol: ["bittorrent"], to: "block" }])
    expect(all.ads[0]).toMatchObject({ domain: ["geosite:category-ads-all"], to: "block" })
    expect(all["ru-direct"]).toHaveLength(2)
    expect(all["ru-direct"].every((r) => r.to === "direct")).toBe(true)
    expect(all["ru-direct"].some((r) => r.domain && r.ip)).toBe(false)
  })

  it("keeps plan rules after the others", () => {
    const rules = orderRules([{ plans: [1], to: "a" }, { domain: ["x.com"], to: "b" }, { plans: [2], to: "c" }, { port: "443", to: "d" }])
    expect(rules.map((r) => r.to)).toEqual(["b", "d", "a", "c"])
  })

  it("finds where a tag is used and renames it everywhere", () => {
    const d = doc()
    expect(usages(d, "nl")).toEqual(["правило 2", "балансировщик best"])
    expect(usages(d, "direct")).toEqual(["балансировщик best"])
    expect(usages(d, "best")).toEqual(["весь остальной трафик"])
    const r = renameTag(d, "nl", "de")
    expect(r.rules[1].to).toBe("de")
    expect(r.balancers[0].outbounds).toEqual(["de", "fi"])
    expect(renameTag(d, "best", "fast").default).toBe("fast")
  })

  it("checks tags like the core", () => {
    const d = doc()
    expect(tagProblem(d, "nl")).toBe("такой тег уже есть")
    expect(tagProblem(d, "nl", "nl")).toBeNull()
    expect(tagProblem(d, "sylph-x")).toContain("sylph")
    expect(tagProblem(d, "Bad Tag")).not.toBeNull()
    expect(freeTag(d, "NL")).toBe("nl-2")
    expect(freeTag(d, "Нидерланды")).toBe("out")
    expect(freeTag(d, "sylph")).toBe("x")
  })

  it("shows conditions as chips", () => {
    const chips = ruleChips({ domain: ["a.com", "b.com", "c.com"], network: "tcp,udp", protocol: ["bittorrent"], plans: [3], to: "x" }, (id) => `тариф ${id}`)
    expect(chips).toEqual([
      { label: "домены", value: "a.com, b.com и ещё 1" },
      { label: "сеть", value: "TCP + UDP" },
      { label: "протокол", value: "BitTorrent" },
      { label: "тариф", value: "тариф 3" },
    ])
  })

  it("lists problems that block saving", () => {
    expect(docProblems(doc())).toEqual([])
    const d = doc()
    d.default = ""
    d.rules[0].to = "gone"
    d.balancers[0].outbounds = ["nl"]
    expect(docProblems(d)).toHaveLength(3)
  })

  it("knows when only plan rules changed", () => {
    const a = doc()
    const b = doc()
    b.rules[1] = { plans: [3, 4], to: "best" }
    expect(restartsXray(a, b)).toBe(false)
    b.rules = b.rules.filter((r) => !r.plans)
    expect(restartsXray(a, b)).toBe(true)
    const c = doc()
    c.rules[0].to = "direct"
    expect(restartsXray(a, c)).toBe(true)
  })

  it("finds exit nodes whose service keys change", () => {
    const a = doc()
    const b = doc()
    expect(changedCascades(a, b)).toEqual([])
    b.outbounds.push({ tag: "de", kind: "cascade", cascade: { node_id: 5 } })
    b.outbounds = b.outbounds.filter((o) => o.tag !== "fi")
    expect(changedCascades(a, b)).toEqual([2, 5])
    expect(changedCascades(null, a)).toEqual([2])
    expect(changedCascades(a, null)).toEqual([2])
  })

  it("explains core errors in place", () => {
    const d = doc()
    expect(describeRoutingError(new ApiError(400, "invalid", "doc.outbounds[2].proxy.address", "must be a public hostname or IP"), d)).toBe("Выход nl, адрес: нужен публичный домен или IP")
    expect(describeRoutingError(new ApiError(400, "invalid", "doc.rules[0].domain", `"ru:x" is not a geo source of domains`), d)).toContain("Правило 1 «Торренты», домены: нет такого источника доменов")
    expect(describeRoutingError(new ApiError(400, "invalid", "doc.balancers[0].outbounds", "needs at least two outbounds"), d)).toBe("Балансировщик best, выходы: нужно хотя бы два выхода")
    expect(describeRoutingError(new ApiError(409, "conflict", "revision", "the profile was changed by someone else: reload it"), d)).toContain("обнови страницу")
    expect(describeRoutingError(new ApiError(400, "invalid", "doc.balancers[0].outbounds", `tag "s" is the beginning of the system outbound "sylph_block": xray would pick it too, rename the outbound`), d)).toContain("системного выхода")
    expect(describeRoutingError(new ApiError(400, "invalid", "doc.outbounds[3].raw", `write "PROTOCOL" in lowercase: xray reads keys in any case`), d)).toContain("строчными")
    expect(describeRoutingError(new ApiError(400, "invalid", "name", "x"), d)).toBeNull()
  })
})
