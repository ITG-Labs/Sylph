import { describe, expect, it } from "vitest"
import { accessChanges, describeChanges, newOldAgentLines, poolCovers, sees, visibleInbounds, withMembers, withNodeGroups, withPlanGroup, type World } from "./groups"

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
  it("a plan sees its group and everything without a group", () => {
    expect(ids(visibleInbounds(world, world.plans[0]))).toEqual([11, 31, 41])
    expect(ids(visibleInbounds(world, world.plans[1]))).toEqual([21, 22, 31, 41])
    expect(ids(visibleInbounds(world, world.plans[2]))).toEqual([22, 31, 41])
  })

  it("an inbound in a group is hidden from other plans even when its node is not grouped", () => {
    const loose = withNodeGroups(world, 2, [])
    expect(sees(loose, loose.plans[0], loose.inbounds[1])).toBe(true)
    expect(sees(loose, loose.plans[0], loose.inbounds[2])).toBe(false)
  })
})

describe("access changes", () => {
  it("taking a node out of its only group gives it to every plan", () => {
    const changes = accessChanges(world, withMembers(world, PREM, [], []))
    expect(changes.map((c) => [c.plan.id, ids(c.lost), ids(c.gained)])).toEqual([
      [10, [], [21]],
      [20, [22], []],
      [30, [], [21]],
    ])
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
    expect(texts(describeChanges(world, accessChanges(world, moved)))).toEqual([
      "30 юзеров тарифа «p20» потеряют ноду «n2»: она перестанет их пускать в течение минуты, а из приложения пропадёт при следующем обновлении подписки.",
      "30 юзеров тарифа «p20» получат ноду «n1» при следующем обновлении подписки.",
    ])
    const loose = withNodeGroups(world, 2, [])
    expect(texts(describeChanges(world, accessChanges(world, loose)))).toEqual([
      "12 юзеров тарифа «p10» получат вход «n2 · main» при следующем обновлении подписки.",
      "30 юзеров тарифа «p20» потеряют вход «n2 · cdn»: он перестанет их пускать в течение минуты, а из приложения пропадёт при следующем обновлении подписки.",
      "Тариф «p30» начнёт видеть вход «n2 · main». Сейчас на нём никого нет.",
    ])
  })

  it("marks nodes outside subscriptions, warns about orphans and says when nothing changes", () => {
    const hidden = withMembers(world, EU, [1, 4], [])
    expect(texts(describeChanges(world, accessChanges(world, hidden)))).toEqual([
      "30 юзеров тарифа «p20» потеряют ноду «n4» (выключена — в подписке её нет): она перестанет их пускать в течение минуты.",
      "Тариф «p30» перестанет видеть ноду «n4» (выключена — в подписке её нет). Сейчас на нём никого нет.",
    ])
    const orphan = withMembers(withMembers(world, EU, [1, 3], []), CDN, [], [])
    const lines = describeChanges(world, accessChanges(world, orphan))
    expect(lines.at(-1)).toEqual({ tone: "danger", text: "У тарифа «p30» в подписке не останется ни одного входа — его юзерам станет не к чему подключиться." })
    expect(texts(describeChanges(world, accessChanges(world, world)))).toEqual(["Доступы юзеров не меняются."])
  })
})

describe("plans without a group", () => {
  it("see only what is in no group, and leaving a group is a move like any other", () => {
    const free = withPlanGroup(world, 10, null)
    expect(ids(visibleInbounds(free, free.plans[0]))).toEqual([31, 41])
    const changes = accessChanges(world, free)
    expect(changes.map((c) => [c.plan.id, ids(c.lost), ids(c.gained)])).toEqual([[10, [11], []]])
  })
})

describe("old agents", () => {
  const old = (w: World): World => ({ ...w, nodes: w.nodes.map((n) => (n.id === 2 ? { ...n, agent_per_inbound: false, last_sync_at: "2026-10-30T10:00:00Z" } : n)) })
  it("warn when a change leaves plans with only part of a node an old agent runs", () => {
    const before = old(world)
    expect(newOldAgentLines(before, before)).toEqual([])
    const split = withNodeGroups(before, 2, [])
    const lines = newOldAgentLines(before, split)
    expect(lines).toHaveLength(1)
    expect(lines[0].tone).toBe("danger")
    expect(lines[0].text).toContain("«n2»")
    expect(lines[0].text).toContain("«p10»")
  })

  it("warn when pools split such a node", () => {
    const before = old(withMembers(world, CDN, [], []))
    const pooled: World = { ...before, plans: before.plans.map((p) => (p.id === 20 ? { ...p, pools: [{ node_id: null, group_id: CDN }] } : p)) }
    const withCDN = withMembers(pooled, CDN, [], [22])
    expect(newOldAgentLines(before, withCDN).map((l) => l.text).join(" ")).toContain("«p20»")
  })

  it("say what a pool covers for a plan", () => {
    expect(ids(poolCovers(world, world.plans[1], { node_id: 2, group_id: null }))).toEqual([21, 22])
    expect(poolCovers(world, world.plans[0], { node_id: 2, group_id: null })).toEqual([])
  })
})
