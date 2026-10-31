import { describe, expect, it } from "vitest"
import {
  accessChanges,
  describeChanges,
  freeInbounds,
  newOldAgentLines,
  plansOutsidePools,
  poolCovers,
  sees,
  ungrouped,
  unseenPoolInbounds,
  visibleInbounds,
  withMembers,
  withNodeGroups,
  withPlanGroup,
  type World,
} from "./groups"

const node = (id: number, enabled = true, address = "x.example") => ({ id, name: `n${id}`, enabled, address, inbounds: 2 })
const inb = (id: number, node_id: number, tag: string) => ({ id, node_id, tag, label: "" })
const plan = (id: number, location_group_id: number, active_users: number) => ({ id, name: `p${id}`, location_group_id, active_users })
const ids = (xs: { id: number }[]) => xs.map((x) => x.id)

const EU = 1
const PREM = 2
const CDN = 3
const world: World = {
  nodes: [node(1), node(2), node(3), node(4, false)],
  inbounds: [inb(11, 1, "main"), inb(21, 2, "main"), inb(22, 2, "cdn"), inb(31, 3, "main"), inb(41, 4, "main")],
  groups: [
    { id: EU, node_ids: [1], inbound_ids: [] },
    { id: PREM, node_ids: [2], inbound_ids: [] },
    { id: CDN, node_ids: [], inbound_ids: [22] },
  ],
  plans: [plan(10, EU, 12), plan(20, PREM, 30), plan(30, CDN, 0)],
}

describe("visibility", () => {
  it("a plan sees only its group, and what is in no group nobody sees", () => {
    expect(ids(visibleInbounds(world, world.plans[0]))).toEqual([11])
    expect(ids(visibleInbounds(world, world.plans[1]))).toEqual([21, 22])
    expect(ids(visibleInbounds(world, world.plans[2]))).toEqual([22])
    expect(ungrouped(world, world.inbounds[3])).toBe(true)
    expect(ungrouped(world, world.inbounds[2])).toBe(false)
  })

  it("an inbound in a group stays with that group when its node leaves another", () => {
    const loose = withNodeGroups(world, 2, [])
    expect(sees(loose, loose.plans[1], loose.inbounds[1])).toBe(false)
    expect(sees(loose, loose.plans[2], loose.inbounds[2])).toBe(true)
    expect(ungrouped(loose, loose.inbounds[1])).toBe(true)
  })
})

describe("access changes", () => {
  it("taking a node out of its only group hides it from everyone", () => {
    const changes = accessChanges(world, withMembers(world, PREM, [], []))
    expect(changes.map((c) => [c.plan.id, ids(c.lost), ids(c.gained), c.orphaned])).toEqual([[20, [21, 22], [], true]])
  })

  it("moving a plan swaps what it sees and spots plans left with nothing live", () => {
    const changes = accessChanges(world, withPlanGroup(world, 10, PREM))
    expect(changes.map((c) => [c.plan.id, ids(c.lost), ids(c.gained), c.orphaned])).toEqual([[10, [11], [21, 22], false]])
    const lonely: World = { ...world, nodes: world.nodes.map((n) => (n.id === 3 ? { ...n, enabled: false } : n)) }
    expect(accessChanges(lonely, withMembers(lonely, EU, [], [11, 31])).some((c) => c.orphaned)).toBe(false)
    const orphaning = withMembers(withMembers(lonely, EU, [], []), PREM, [2, 3], [11])
    expect(accessChanges(lonely, orphaning).find((c) => c.plan.id === 10)?.orphaned).toBe(true)
  })
})

describe("change texts", () => {
  const texts = (lines: { text: string }[]) => lines.map((l) => l.text)

  it("names whole nodes and single inbounds", () => {
    const shared = withMembers(world, CDN, [], [22, 11])
    expect(texts(describeChanges(world, accessChanges(world, shared)))).toEqual(["Тариф «p30» начнёт видеть ноду «n1». Сейчас на нём никого нет."])
    const moved = withPlanGroup(world, 20, EU)
    expect(texts(describeChanges(world, accessChanges(world, moved)))).toHaveLength(2)
    const partly = withMembers(world, PREM, [], [21])
    expect(texts(describeChanges(world, accessChanges(world, partly)))).toEqual([expect.stringContaining("вход «n2 · cdn»")])
  })

  it("warns about orphans and says when nothing changes", () => {
    const orphan = withMembers(withMembers(world, EU, [1, 3], []), CDN, [], [])
    const lines = describeChanges(world, accessChanges(world, orphan))
    expect(lines.at(-1)?.tone).toBe("danger")
    expect(lines.at(-1)?.text).toContain("«p30»")
    expect(describeChanges(world, accessChanges(world, world))).toHaveLength(1)
    expect(describeChanges(world, accessChanges(world, world))[0].tone).toBe("info")
  })
})

describe("plans without a group", () => {
  it("see nothing", () => {
    const none = withPlanGroup(world, 10, null)
    expect(visibleInbounds(none, none.plans[0])).toEqual([])
    const changes = accessChanges(world, none)
    expect(changes.map((c) => [c.plan.id, ids(c.lost), ids(c.gained), c.orphaned])).toEqual([[10, [11], [], true]])
  })
})

describe("pools", () => {
  const pooled = (w: World, planID: number, pools: number[][]): World => ({
    ...w,
    plans: w.plans.map((p) => (p.id === planID ? { ...p, pools: pools.map((inbound_ids) => ({ inbound_ids })) } : p)),
  })

  it("cover only what the plan sees and leave the rest free", () => {
    const w = pooled(world, 20, [[21, 31]])
    const p20 = w.plans[1]
    expect(ids(poolCovers(w, p20, { inbound_ids: [21, 31] }))).toEqual([21])
    expect(ids(freeInbounds(w, p20))).toEqual([22])
    expect(ids(unseenPoolInbounds(w, p20))).toEqual([31])
  })

  it("warn when a change splits a node an old agent runs", () => {
    const old = (w: World): World => ({ ...w, nodes: w.nodes.map((n) => (n.id === 2 ? { ...n, agent_per_inbound: false, last_sync_at: "2026-10-30T10:00:00Z" } : n)) })
    const before = old(withMembers(world, CDN, [], []))
    expect(newOldAgentLines(before, before)).toEqual([])
    const partly = newOldAgentLines(before, withMembers(before, PREM, [], [21]))
    expect(partly).toHaveLength(1)
    expect(partly[0].tone).toBe("danger")
    expect(partly[0].text).toContain("«n2»")
    expect(partly[0].text).toContain("«p20»")
    expect(newOldAgentLines(before, pooled(before, 20, [[22]])).map((l) => l.text).join(" ")).toContain("«p20»")
    expect(newOldAgentLines(before, pooled(before, 20, [[21, 22]]))).toEqual([])
    const xhttpOnly: World = { ...before, inbounds: before.inbounds.map((i) => (i.id === 22 ? { ...i, transport: "xhttp" } : i)) }
    expect(newOldAgentLines(xhttpOnly, withMembers(xhttpOnly, PREM, [], [21]))).toEqual([])
  })

  it("warn when a limited plan gains inbounds outside its pools", () => {
    const limited: World = { ...world, plans: world.plans.map((p) => (p.id === 10 ? { ...p, pools: [{ inbound_ids: [11], traffic_limit_bytes: 100 }] } : p)) }
    const lines = describeChanges(limited, accessChanges(limited, withMembers(limited, EU, [1, 3], [])))
    expect(lines.some((l) => l.text.includes("не входят в его пулы") && l.text.includes("«p10»"))).toBe(true)
    expect(plansOutsidePools(limited, 1).map((p) => p.id)).toEqual([10])
    expect(plansOutsidePools(limited, 2)).toEqual([])
    const unlimited: World = { ...world, plans: world.plans.map((p) => (p.id === 10 ? { ...p, pools: [{ inbound_ids: [11], traffic_limit_bytes: null }] } : p)) }
    expect(plansOutsidePools(unlimited, 1)).toEqual([])
  })
})
