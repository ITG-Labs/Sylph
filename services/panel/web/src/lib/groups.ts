import { plural } from "./format"

export interface NodeLike {
  id: number
  name: string
  enabled: boolean
  address: string
  inbounds: number
}

export interface InboundLike {
  id: number
  node_id: number
  tag: string
  label: string
}

export interface GroupLike {
  id: number
  node_ids: number[]
  inbound_ids: number[]
}

export interface PlanLike {
  id: number
  name: string
  location_group_id: number
  active_users: number
}

export interface World {
  nodes: NodeLike[]
  inbounds: InboundLike[]
  groups: GroupLike[]
  plans: PlanLike[]
}

export interface PlanChange {
  plan: PlanLike
  lost: InboundLike[]
  gained: InboundLike[]
  orphaned: boolean
}

export interface ImpactLine {
  tone: "lose" | "gain" | "info" | "danger"
  text: string
}

export function hiddenReason(n: NodeLike): string | null {
  if (!n.enabled) return "выключена"
  if (n.address === "") return "без адреса"
  if (n.inbounds === 0) return "без входов"
  return null
}

export function groupsOf(w: World, inbound: InboundLike): number[] {
  return w.groups.filter((g) => g.inbound_ids.includes(inbound.id) || g.node_ids.includes(inbound.node_id)).map((g) => g.id)
}

export function sees(w: World, plan: PlanLike, inbound: InboundLike): boolean {
  const groups = groupsOf(w, inbound)
  return groups.length === 0 || groups.includes(plan.location_group_id)
}

export function visibleInbounds(w: World, plan: PlanLike): InboundLike[] {
  return w.inbounds.filter((i) => sees(w, plan, i))
}

export function withMembers(w: World, groupID: number, nodeIDs: number[], inboundIDs: number[]): World {
  return { ...w, groups: w.groups.map((g) => (g.id === groupID ? { ...g, node_ids: nodeIDs, inbound_ids: inboundIDs } : g)) }
}

export function withNodeGroups(w: World, nodeID: number, groupIDs: number[]): World {
  return {
    ...w,
    groups: w.groups.map((g) => ({
      ...g,
      node_ids: groupIDs.includes(g.id) ? [...new Set([...g.node_ids, nodeID])] : g.node_ids.filter((id) => id !== nodeID),
    })),
  }
}

export function withPlanGroup(w: World, planID: number, groupID: number): World {
  return { ...w, plans: w.plans.map((p) => (p.id === planID ? { ...p, location_group_id: groupID } : p)) }
}

const nodeOf = (w: World, i: InboundLike) => w.nodes.find((n) => n.id === i.node_id)
const live = (w: World, i: InboundLike) => {
  const n = nodeOf(w, i)
  return !!n && n.enabled && n.address !== ""
}

export function accessChanges(before: World, after: World): PlanChange[] {
  const out: PlanChange[] = []
  for (const was of before.plans) {
    const now = after.plans.find((p) => p.id === was.id)
    if (!now) continue
    const seenBefore = visibleInbounds(before, was)
    const seenAfter = visibleInbounds(after, now)
    const lost = seenBefore.filter((i) => !seenAfter.some((x) => x.id === i.id))
    const gained = seenAfter.filter((i) => !seenBefore.some((x) => x.id === i.id))
    const orphaned = seenBefore.some((i) => live(before, i)) && !seenAfter.some((i) => live(after, i))
    if (lost.length > 0 || gained.length > 0 || orphaned) out.push({ plan: was, lost, gained, orphaned })
  }
  return out
}

const q = (s: string) => `«${s}»`
const usersCount = (n: number) => `${n} ${plural(n, "юзер", "юзера", "юзеров")}`

interface Items {
  text: string
  live: boolean
  pronoun: string
  many: boolean
}

function items(w: World, list: InboundLike[]): Items {
  const nodes: string[] = []
  const inbounds: string[] = []
  const done = new Set<number>()
  for (const i of list) {
    if (done.has(i.node_id)) continue
    const node = nodeOf(w, i)
    const all = w.inbounds.filter((x) => x.node_id === i.node_id)
    const why = node ? hiddenReason(node) : null
    if (all.every((x) => list.some((l) => l.id === x.id))) {
      done.add(i.node_id)
      nodes.push(why === null ? q(node?.name ?? `нода ${i.node_id}`) : `${q(node?.name ?? "")} (${why} — в подписке её нет)`)
      continue
    }
    const name = `${node?.name ?? `нода ${i.node_id}`} · ${i.label || i.tag}`
    inbounds.push(why === null ? q(name) : `${q(name)} (нода ${why} — в подписке его нет)`)
  }
  const parts: string[] = []
  if (nodes.length > 0) parts.push(`${nodes.length === 1 ? "ноду" : "ноды"} ${nodes.join(", ")}`)
  if (inbounds.length > 0) parts.push(`${inbounds.length === 1 ? "вход" : "входы"} ${inbounds.join(", ")}`)
  const many = nodes.length + inbounds.length > 1
  return {
    text: parts.join(" и "),
    live: list.some((i) => live(w, i)),
    pronoun: many ? "они" : nodes.length === 1 ? "она" : "он",
    many,
  }
}

export function describeChanges(before: World, changes: PlanChange[]): ImpactLine[] {
  const out: ImpactLine[] = []
  for (const c of changes) {
    const n = c.plan.active_users
    const who = `${usersCount(n)} тарифа ${q(c.plan.name)}`
    if (c.lost.length > 0) {
      const it = items(before, c.lost)
      const stops = `${it.pronoun} ${it.many ? "перестанут" : "перестанет"} ${plural(n, "его", "их", "их")} пускать в течение минуты`
      const fromApp = it.live ? `, а из приложения ${it.many ? "пропадут" : "пропадёт"} при следующем обновлении подписки` : ""
      out.push(
        n === 0
          ? { tone: "info", text: `Тариф ${q(c.plan.name)} перестанет видеть ${it.text}. Сейчас на нём никого нет.` }
          : { tone: "lose", text: `${who} ${plural(n, "потеряет", "потеряют", "потеряют")} ${it.text}: ${stops}${fromApp}.` },
      )
    }
    if (c.gained.length > 0) {
      const it = items(before, c.gained)
      out.push(
        n === 0
          ? { tone: "info", text: `Тариф ${q(c.plan.name)} начнёт видеть ${it.text}. Сейчас на нём никого нет.` }
          : it.live
            ? { tone: "gain", text: `${who} ${plural(n, "получит", "получат", "получат")} ${it.text} при следующем обновлении подписки.` }
            : { tone: "gain", text: `${who} ${plural(n, "получит", "получат", "получат")} ${it.text}, но в подписке это появится, только когда нода станет доступна.` },
      )
    }
    if (c.orphaned) out.push({ tone: "danger", text: `У тарифа ${q(c.plan.name)} в подписке не останется ни одного входа — его юзерам станет не к чему подключиться.` })
  }
  if (out.length === 0) out.push({ tone: "info", text: "Доступы юзеров не меняются." })
  return out
}

export function isHarsh(lines: ImpactLine[]): boolean {
  return lines.some((l) => l.tone === "danger" || l.tone === "lose")
}
