import type { World } from "./groups"
import { useGroups, useInboundRefs, useNodes, usePlans } from "./queries"

export function useWorld() {
  const groups = useGroups()
  const nodes = useNodes()
  const inbounds = useInboundRefs()
  const plans = usePlans()
  const queries = [groups, nodes, inbounds, plans]
  const failed = queries.find((q) => q.isError)
  const world: World | undefined =
    groups.data && nodes.data && inbounds.data && plans.data
      ? { groups: groups.data, nodes: nodes.data, inbounds: inbounds.data, plans: plans.data.map((p) => ({ ...p, active_users: p.active_users ?? 0 })) }
      : undefined
  return { world, groups: groups.data, plans: plans.data, error: failed?.error ?? null, retry: () => failed?.refetch() }
}
