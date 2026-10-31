const actions: Record<string, Record<string, string>> = {
  user: { ban: "заблокировал пользователя", unban: "разблокировал пользователя", extend: "продлил подписку", traffic: "добавил трафик", plan: "сменил тариф пользователя" },
  users: { rotate: "сменил ссылку подписки", reset: "отвязал все устройства", devices: "отвязал устройство" },
  subscription: { close: "закрыл период" },
  payment: { recheck: "перепроверил платёж", apply: "провёл платёж", reject: "отклонил платёж", refund: "отметил возврат" },
  payments: { post: "выдал период вручную", recheck: "перепроверил платёж в ЮKassa" },
  plan: { create: "создал тариф", update: "изменил тариф" },
  plan_access: { grant: "открыл доступ к тарифу", revoke: "отозвал доступ к тарифу" },
  invite: { create: "создал приглашение", revoke: "отозвал приглашение" },
  node: { create: "добавил ноду", update: "изменил ноду" },
  inbound: { create: "создал вход", update: "изменил вход", delete: "удалил вход" },
  group: { create: "создал группу", update: "переименовал группу", delete: "удалил группу", members: "изменил состав группы" },
  settings: { update: "изменил настройки", terms_bump: "запросил повторное принятие соглашения" },
}

const entities: Record<string, string> = {
  user: "пользователь",
  users: "пользователь",
  subscription: "период",
  payment: "платёж",
  payments: "платёж",
  plan: "тариф",
  plan_access: "тариф",
  invite: "приглашение",
  node: "нода",
  inbound: "вход",
  group: "группа",
  settings: "настройки",
}

export const auditEntities = [
  ["user", "Пользователи: блокировка, продление, трафик, тариф"],
  ["users", "Пользователи: ссылка и устройства"],
  ["subscription", "Периоды"],
  ["payment", "Платежи"],
  ["payments", "Ручная выдача"],
  ["plan", "Тарифы"],
  ["plan_access", "Доступы к тарифам"],
  ["invite", "Приглашения"],
  ["node", "Ноды"],
  ["inbound", "Входы"],
  ["group", "Группы"],
  ["settings", "Настройки"],
]

export function auditAction(entity: string, action: string): string {
  return actions[entity]?.[action] ?? `${action} ${entity}`
}

export function auditEntity(entity: string): string {
  return entities[entity] ?? entity
}

export function auditLink(entity: string, id: string): string | null {
  if (!id) return null
  switch (entity) {
    case "user":
    case "users":
      return `/users/${id}`
    case "payment":
    case "payments":
      return `/payments/${encodeURIComponent(id)}`
    case "plan":
    case "plan_access":
      return `/plans/${id}`
    case "node":
      return `/nodes/${id}`
    case "group":
      return `/groups/${id}`
    default:
      return null
  }
}

export function auditDetails(d: Record<string, unknown> | null): string {
  if (!d) return ""
  return Object.entries(d)
    .map(([k, v]) => `${k}: ${typeof v === "object" ? JSON.stringify(v) : String(v)}`)
    .join(" · ")
}
