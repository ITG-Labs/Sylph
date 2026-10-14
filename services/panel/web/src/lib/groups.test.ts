import { describe, expect, it } from "vitest"
import { describeNodeMove, describePlanMove, nodeMoveImpact, planMoveImpact, sees } from "./groups"

const node = (id: number, group_id: number | null, enabled = true, inbounds = 1, address = "x.example") => ({ id, name: `n${id}`, group_id, enabled, inbounds, address })
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

  it("nodes outside subscriptions do not keep a plan alive", () => {
    const eu = node(1, EU)
    expect(ids(planMoveImpact(plans[0], PREM, [eu, node(5, PREM, false)]).orphaned)).toEqual([10])
    expect(ids(planMoveImpact(plans[0], PREM, [eu, node(6, PREM, true, 0)]).orphaned)).toEqual([10])
    expect(ids(planMoveImpact(plans[0], PREM, [eu, node(7, PREM, true, 1, "")]).orphaned)).toEqual([10])
    expect(planMoveImpact(plans[0], PREM, [eu, node(8, PREM)]).orphaned).toEqual([])
  })

  it("does not warn about plans that had nothing to lose", () => {
    const hidden = [node(5, PREM, false), node(6, PREM, true, 0)]
    expect(planMoveImpact(plans[2], null, hidden).orphaned).toEqual([])
    expect(nodeMoveImpact(hidden[1], EU, plans, hidden).orphaned).toEqual([])
  })
})

describe("move texts", () => {
  const texts = (lines: { text: string }[]) => lines.map((l) => l.text)

  it("tells who loses and who gains a moved node", () => {
    const lines = describeNodeMove(nodes[0], PREM, nodeMoveImpact(nodes[0], PREM, plans, nodes))
    expect(texts(lines)).toEqual([
      "15 юзеров на тарифах «p10», «p11» потеряют ноду «n1»: она перестанет их пускать в течение минуты, а из приложения пропадёт при следующем обновлении подписки.",
      "30 юзеров на тарифе «p20» получат ноду «n1» при следующем обновлении подписки.",
    ])
    expect(lines.map((l) => l.tone)).toEqual(["lose", "gain"])
  })

  it("agrees the verb with one user and says nothing changes", () => {
    const one = [plan(10, EU, 21), plan(20, PREM, 1)]
    expect(texts(describeNodeMove(nodes[0], PREM, nodeMoveImpact(nodes[0], PREM, one, nodes)))).toEqual([
      "21 юзер на тарифе «p10» потеряет ноду «n1»: она перестанет его пускать в течение минуты, а из приложения пропадёт при следующем обновлении подписки.",
      "1 юзер на тарифе «p20» получит ноду «n1» при следующем обновлении подписки.",
    ])
    expect(texts(describeNodeMove(nodes[0], EU, nodeMoveImpact(nodes[0], EU, plans, nodes)))).toEqual(["Доступы юзеров не меняются."])
  })

  it("warns about a common node, empty plans and plans left without nodes", () => {
    const empty = [plan(20, PREM, 0), plan(30, null, 0)]
    const lines = describeNodeMove(nodes[3], EU, nodeMoveImpact(nodes[3], EU, empty, nodes))
    expect(texts(lines)).toEqual([
      "На тарифах «p20», «p30» сейчас никого нет — нода просто уйдёт из них.",
      "У тарифа «p30» в подписке не останется ни одной ноды — его юзерам станет не к чему подключиться.",
    ])
    expect(lines[1].tone).toBe("danger")
    expect(texts(describeNodeMove(nodes[2], null, nodeMoveImpact(nodes[2], null, plans, nodes)))[0]).toBe(
      "Нода станет общей: её видят все тарифы, в том числе созданные позже.",
    )
  })

  it("tells a plan which nodes it loses and gains", () => {
    expect(texts(describePlanMove(plans[0], PREM, planMoveImpact(plans[0], PREM, nodes)))).toEqual([
      "12 юзеров тарифа «p10» потеряют ноды «n1», «n2»: они перестанут их пускать в течение минуты, а из приложения пропадут при следующем обновлении подписки.",
      "12 юзеров тарифа «p10» получат ноды «n3», «n5» (выключена — в подписке её нет) при следующем обновлении подписки.",
    ])
    const lonely = plan(40, EU, 0)
    expect(texts(describePlanMove(lonely, null, planMoveImpact(lonely, null, nodes.filter((n) => n.id !== 4))))).toEqual([
      "Без группы тариф видит только общие ноды.",
      "Тариф перестанет видеть ноды «n1», «n2». Сейчас на нём никого нет.",
      "У тарифа в подписке не останется ни одной ноды — его юзерам станет не к чему подключиться.",
    ])
  })

  it("never promises an app change for a node outside subscriptions", () => {
    const off = node(9, EU, false)
    const all = [...nodes, off]
    expect(texts(describeNodeMove(off, PREM, nodeMoveImpact(off, PREM, plans, all)))).toEqual([
      "Нода выключена — в подписках её нет, у юзеров в приложениях ничего не поменяется. Пускать она будет уже по новой группе.",
      "15 юзеров на тарифах «p10», «p11» потеряют доступ к ноде «n9»: она перестанет их пускать в течение минуты.",
      "Нода начнёт пускать 30 юзеров на тарифе «p20», но в приложениях появится, только когда её включат.",
    ])
    const bare = node(10, null, true, 0)
    expect(texts(describeNodeMove(bare, EU, nodeMoveImpact(bare, EU, [plan(10, EU, 1)], [bare])))[0]).toBe(
      "Нода без входов — в подписках её нет, у юзеров в приложениях ничего не поменяется. Пускать она будет уже по новой группе.",
    )
    expect(texts(describeNodeMove(node(11, EU, true, 1, ""), null, nodeMoveImpact(node(11, EU, true, 1, ""), null, [plan(30, null, 1)], nodes))).at(-1)).toBe(
      "Нода начнёт пускать 1 юзера на тарифе «p30», но в приложениях появится, только когда у неё появится адрес.",
    )
    const onlyOff = [node(1, EU), node(12, PREM, false)]
    expect(texts(describePlanMove(plans[0], PREM, planMoveImpact(plans[0], PREM, onlyOff)))).toEqual([
      "12 юзеров тарифа «p10» потеряют ноду «n1»: она перестанет их пускать в течение минуты, а из приложения пропадёт при следующем обновлении подписки.",
      "12 юзеров тарифа «p10» получат ноду «n12» (выключена — в подписке её нет), но в подписку она попадёт, только когда станет доступна.",
      "У тарифа в подписке не останется ни одной ноды — его юзерам станет не к чему подключиться.",
    ])
  })
})
