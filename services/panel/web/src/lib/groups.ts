export interface NodeLike {
  id: number
  name: string
  group_id: number | null
  enabled: boolean
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

const hasLiveNode = (planGroup: number | null, nodes: NodeLike[]) => nodes.some((n) => n.enabled && sees(planGroup, n.group_id))

export function nodeMoveImpact(node: NodeLike, to: number | null, plans: PlanLike[], nodes: NodeLike[]): MoveImpact {
  const before = plans.filter((p) => sees(p.location_group_id, node.group_id))
  const after = plans.filter((p) => sees(p.location_group_id, to))
  const lose = before.filter((p) => !after.includes(p))
  const gain = after.filter((p) => !before.includes(p))
  const moved = nodes.map((n) => (n.id === node.id ? { ...n, group_id: to } : n))
  return {
    lose: { plans: lose, nodes: [node], users: users(lose) },
    gain: { plans: gain, nodes: [node], users: users(gain) },
    orphaned: lose.filter((p) => !hasLiveNode(p.location_group_id, moved)),
  }
}

export function planMoveImpact(plan: PlanLike, to: number | null, nodes: NodeLike[]): MoveImpact {
  const before = nodes.filter((n) => sees(plan.location_group_id, n.group_id))
  const after = nodes.filter((n) => sees(to, n.group_id))
  return {
    lose: { plans: [plan], nodes: before.filter((n) => !after.includes(n)), users: plan.active_users },
    gain: { plans: [plan], nodes: after.filter((n) => !before.includes(n)), users: plan.active_users },
    orphaned: hasLiveNode(to, nodes) ? [] : [plan],
  }
}
