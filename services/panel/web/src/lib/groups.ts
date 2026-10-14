import { plural } from "./format"

export interface NodeLike {
  id: number
  name: string
  group_id: number | null
  enabled: boolean
  address: string
  inbounds: number
}

export interface PlanLike {
  id: number
  name: string
  location_group_id: number | null
  active_users: number
}

export interface ImpactSide {
  plans: PlanLike[]
  nodes: NodeLike[]
  users: number
}

export interface MoveImpact {
  lose: ImpactSide
  gain: ImpactSide
  orphaned: PlanLike[]
}

export function sees(planGroup: number | null, nodeGroup: number | null): boolean {
  return nodeGroup === null || nodeGroup === planGroup
}

const users = (plans: PlanLike[]) => plans.reduce((sum, p) => sum + p.active_users, 0)

export function hiddenReason(n: NodeLike): string | null {
  if (!n.enabled) return "выключена"
  if (n.address === "") return "без адреса"
  if (n.inbounds === 0) return "без входов"
  return null
}

const inSubscription = (n: NodeLike) => hiddenReason(n) === null

const hasLiveNode = (planGroup: number | null, nodes: NodeLike[]) => nodes.some((n) => inSubscription(n) && sees(planGroup, n.group_id))

export function nodeMoveImpact(node: NodeLike, to: number | null, plans: PlanLike[], nodes: NodeLike[]): MoveImpact {
  const before = plans.filter((p) => sees(p.location_group_id, node.group_id))
  const after = plans.filter((p) => sees(p.location_group_id, to))
  const lose = before.filter((p) => !after.includes(p))
  const gain = after.filter((p) => !before.includes(p))
  const moved = nodes.map((n) => (n.id === node.id ? { ...n, group_id: to } : n))
  return {
    lose: { plans: lose, nodes: [node], users: users(lose) },
    gain: { plans: gain, nodes: [node], users: users(gain) },
    orphaned: lose.filter((p) => hasLiveNode(p.location_group_id, nodes) && !hasLiveNode(p.location_group_id, moved)),
  }
}

export function planMoveImpact(plan: PlanLike, to: number | null, nodes: NodeLike[]): MoveImpact {
  const before = nodes.filter((n) => sees(plan.location_group_id, n.group_id))
  const after = nodes.filter((n) => sees(to, n.group_id))
  return {
    lose: { plans: [plan], nodes: before.filter((n) => !after.includes(n)), users: plan.active_users },
    gain: { plans: [plan], nodes: after.filter((n) => !before.includes(n)), users: plan.active_users },
    orphaned: hasLiveNode(plan.location_group_id, nodes) && !hasLiveNode(to, nodes) ? [plan] : [],
  }
}

export interface ImpactLine {
  tone: "lose" | "gain" | "info" | "danger"
  text: string
}

const q = (s: string) => `«${s}»`
const nodeLabel = (n: NodeLike) => {
  const why = hiddenReason(n)
  return why === null ? q(n.name) : `${q(n.name)} (${why} — в подписке её нет)`
}
const unlockWhen = (n: NodeLike) => (!n.enabled ? "когда её включат" : n.address === "" ? "когда у неё появится адрес" : "когда у неё появится вход")
const usersCount = (n: number) => `${n} ${plural(n, "юзер", "юзера", "юзеров")}`
const onPlans = (plans: PlanLike[]) => (plans.length === 1 ? `тарифе ${q(plans[0].name)}` : `тарифах ${plans.map((p) => q(p.name)).join(", ")}`)
const nodesWord = (nodes: NodeLike[]) => (nodes.length === 1 ? `ноду ${nodeLabel(nodes[0])}` : `ноды ${nodes.map(nodeLabel).join(", ")}`)

function orphanLine(plans: PlanLike[]): ImpactLine {
  const whom = plans.length === 1 ? `тарифа ${q(plans[0].name)}` : `тарифов ${plans.map((p) => q(p.name)).join(", ")}`
  return { tone: "danger", text: `У ${whom} в подписке не останется ни одной ноды — ${plans.length === 1 ? "его" : "их"} юзерам станет не к чему подключиться.` }
}

export function describeNodeMove(node: NodeLike, to: number | null, impact: MoveImpact): ImpactLine[] {
  const out: ImpactLine[] = []
  const { lose, gain } = impact
  if (to === null && node.group_id !== null) out.push({ tone: "info", text: "Нода станет общей: её видят все тарифы, в том числе созданные позже." })
  const why = hiddenReason(node)
  if (why !== null) out.push({ tone: "info", text: `Нода ${why} — в подписках её нет, у юзеров в приложениях ничего не поменяется. Пускать она будет уже по новой группе.` })
  if (lose.plans.length > 0) {
    const n = lose.users
    const them = plural(n, "его", "их", "их")
    out.push(
      n === 0
        ? { tone: "info", text: `На ${onPlans(lose.plans)} сейчас никого нет — нода просто уйдёт из ${lose.plans.length === 1 ? "него" : "них"}.` }
        : why !== null
          ? { tone: "lose", text: `${usersCount(n)} на ${onPlans(lose.plans)} ${plural(n, "потеряет", "потеряют", "потеряют")} доступ к ноде ${q(node.name)}: она перестанет ${them} пускать в течение минуты.` }
          : {
              tone: "lose",
              text: `${usersCount(n)} на ${onPlans(lose.plans)} ${plural(n, "потеряет", "потеряют", "потеряют")} ноду ${q(node.name)}: она перестанет ${them} пускать в течение минуты, а из приложения пропадёт при следующем обновлении подписки.`,
            },
    )
  }
  if (gain.plans.length > 0) {
    const n = gain.users
    out.push(
      n === 0
        ? { tone: "info", text: `Нода добавится в ${gain.plans.length === 1 ? `тариф ${q(gain.plans[0].name)}` : `тарифы ${gain.plans.map((p) => q(p.name)).join(", ")}`}, но на ${gain.plans.length === 1 ? "нём" : "них"} сейчас никого нет.` }
        : why !== null
          ? { tone: "gain", text: `Нода начнёт пускать ${n} ${plural(n, "юзера", "юзеров", "юзеров")} на ${onPlans(gain.plans)}, но в приложениях появится, только ${unlockWhen(node)}.` }
          : { tone: "gain", text: `${usersCount(n)} на ${onPlans(gain.plans)} ${plural(n, "получит", "получат", "получат")} ноду ${q(node.name)} при следующем обновлении подписки.` },
    )
  }
  if (impact.orphaned.length > 0) out.push(orphanLine(impact.orphaned))
  if (lose.plans.length === 0 && gain.plans.length === 0) out.push({ tone: "info", text: "Доступы юзеров не меняются." })
  return out
}

export function describePlanMove(plan: PlanLike, to: number | null, impact: MoveImpact): ImpactLine[] {
  const out: ImpactLine[] = []
  const { lose, gain } = impact
  const n = plan.active_users
  const who = `${usersCount(n)} тарифа ${q(plan.name)}`
  if (to === null && plan.location_group_id !== null) out.push({ tone: "info", text: "Без группы тариф видит только общие ноды." })
  if (lose.nodes.length > 0) {
    const many = lose.nodes.length > 1
    const fromApp = lose.nodes.some(inSubscription) ? `, а из приложения ${many ? "пропадут" : "пропадёт"} при следующем обновлении подписки` : ""
    out.push(
      n === 0
        ? { tone: "info", text: `Тариф перестанет видеть ${nodesWord(lose.nodes)}. Сейчас на нём никого нет.` }
        : {
            tone: "lose",
            text: `${who} ${plural(n, "потеряет", "потеряют", "потеряют")} ${nodesWord(lose.nodes)}: ${many ? "они перестанут" : "она перестанет"} ${plural(n, "его", "их", "их")} пускать в течение минуты${fromApp}.`,
          },
    )
  }
  if (gain.nodes.length > 0) {
    const one = gain.nodes.length === 1
    out.push(
      n === 0
        ? { tone: "info", text: `Тариф начнёт видеть ${nodesWord(gain.nodes)}. Сейчас на нём никого нет.` }
        : gain.nodes.some(inSubscription)
          ? { tone: "gain", text: `${who} ${plural(n, "получит", "получат", "получат")} ${nodesWord(gain.nodes)} при следующем обновлении подписки.` }
          : {
              tone: "gain",
              text: `${who} ${plural(n, "получит", "получат", "получат")} ${nodesWord(gain.nodes)}, но в подписку ${one ? "она попадёт" : "они попадут"}, только когда ${one ? "станет доступна" : "станут доступны"}.`,
            },
    )
  }
  if (impact.orphaned.length > 0) out.push({ tone: "danger", text: "У тарифа в подписке не останется ни одной ноды — его юзерам станет не к чему подключиться." })
  if (lose.nodes.length === 0 && gain.nodes.length === 0) out.push({ tone: "info", text: "Ноды тарифа не меняются." })
  return out
}

export function isHarsh(lines: ImpactLine[]): boolean {
  return lines.some((l) => l.tone === "danger" || l.tone === "lose")
}
