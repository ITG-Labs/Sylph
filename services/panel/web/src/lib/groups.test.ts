import { describe, expect, it } from "vitest"
import { nodeMoveImpact, planMoveImpact, sees } from "./groups"

const node = (id: number, group_id: number | null, enabled = true) => ({ id, name: `n${id}`, group_id, enabled })
const plan = (id: number, location_group_id: number | null, active_users: number) => ({ id, name: `p${id}`, location_group_id, active_users })
const ids = (xs: { id: number }[]) => xs.map((x) => x.id)

const EU = 1
const PREM = 2
const nodes = [node(1, EU), node(2, EU), node(3, PREM), node(4, null), node(5, PREM, false)]
const plans = [plan(10, EU, 12), plan(11, EU, 3), plan(20, PREM, 30), plan(30, null, 5)]

describe("groups", () => {
  it("knows who sees a node", () => {
    expect(sees(EU, null)).toBe(true)
    expect(sees(null, null)).toBe(true)
    expect(sees(EU, EU)).toBe(true)
    expect(sees(EU, PREM)).toBe(false)
    expect(sees(null, EU)).toBe(false)
  })

  it("moving a node between groups swaps its plans", () => {
    const r = nodeMoveImpact(nodes[0], PREM, plans, nodes)
    expect(ids(r.lose.plans)).toEqual([10, 11])
    expect(r.lose.users).toBe(15)
    expect(ids(r.lose.nodes)).toEqual([1])
    expect(ids(r.gain.plans)).toEqual([20])
    expect(r.gain.users).toBe(30)
    expect(r.orphaned).toEqual([])
  })

  it("making a node common gives it to every other plan and takes it from nobody", () => {
    const r = nodeMoveImpact(nodes[2], null, plans, nodes)
    expect(r.lose.plans).toEqual([])
    expect(ids(r.gain.plans)).toEqual([10, 11, 30])
    expect(r.gain.users).toBe(20)
  })

  it("putting a common node into a group takes it from everyone else", () => {
    const r = nodeMoveImpact(nodes[3], EU, plans, nodes)
    expect(ids(r.lose.plans)).toEqual([20, 30])
    expect(r.gain.plans).toEqual([])
    expect(ids(r.orphaned)).toEqual([30])
  })

  it("flags plans left without enabled nodes", () => {
    const r = nodeMoveImpact(nodes[2], EU, plans, nodes)
    expect(ids(r.lose.plans)).toEqual([20])
    expect(r.orphaned).toEqual([])
    const noCommon = nodes.filter((n) => n.id !== 4)
    expect(ids(nodeMoveImpact(nodes[2], EU, plans, noCommon).orphaned)).toEqual([20])
  })

  it("a move to the same group changes nothing", () => {
    const r = nodeMoveImpact(nodes[0], EU, plans, nodes)
    expect(r.lose.plans).toEqual([])
    expect(r.gain.plans).toEqual([])
  })

  it("moving a plan swaps the group nodes and keeps the common ones", () => {
    const r = planMoveImpact(plans[0], PREM, nodes)
    expect(ids(r.lose.nodes)).toEqual([1, 2])
    expect(ids(r.gain.nodes)).toEqual([3, 5])
    expect(r.lose.users).toBe(12)
    expect(r.gain.users).toBe(12)
    expect(ids(r.lose.plans)).toEqual([10])
    expect(r.orphaned).toEqual([])
  })

  it("a plan without a group keeps only common nodes", () => {
    const r = planMoveImpact(plans[2], null, nodes)
    expect(ids(r.lose.nodes)).toEqual([3, 5])
    expect(r.gain.nodes).toEqual([])
    expect(r.orphaned).toEqual([])
    const noCommon = nodes.filter((n) => n.id !== 4)
    expect(ids(planMoveImpact(plans[2], null, noCommon).orphaned)).toEqual([20])
  })

  it("disabled nodes do not keep a plan alive", () => {
    const onlyDisabled = [node(5, PREM, false)]
    expect(ids(planMoveImpact(plans[0], PREM, onlyDisabled).orphaned)).toEqual([10])
  })
})
