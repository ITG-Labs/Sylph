export type NodeStatus = "online" | "degraded" | "offline" | "never" | "disabled"

export interface Overview {
  nodes: {
    total: number
    by_status: Record<NodeStatus, number>
    active_users: number
    nodes: { id: number; name: string; status: NodeStatus; active_users: number | null; traffic_today: number }[]
  }
  users: {
    total: number
    active_subscription: number
    trial: number
    banned: number
    new_today: number
    new_7d: number
    new_30d: number
  }
  revenue: {
    yookassa: { today: number; days_7: number; days_30: number }
    manual: { today: number; days_7: number; days_30: number }
    by_plan_30d: { plan_id: number; name: string; count: number; amount_rub: number }[]
    refunded_30d: number
  }
  attention: { mismatch: number; stuck_pending: number; paid_while_banned: number; nodes_problem: number }
}

export interface UserRow {
  id: number
  telegram_id: number | null
  status: "active" | "banned"
  banned_at: string | null
  created_at: string
  plan: { id: number; name: string } | null
  paid_until: string | null
  traffic_used_bytes: number
  traffic_limit_bytes: number | null
  devices: number
  telegram_username: string | null
  telegram_name: string | null
}

export interface UsersPage {
  users: UserRow[]
  next_before: number | null
}

export interface User {
  id: number
  telegram_id: number | null
  sub_token: string
  status: "active" | "banned"
  banned_at: string | null
  ban_reason: string | null
  created_at: string
}

export type PeriodState = "current" | "queued" | "closed" | "expired" | string

export interface Period {
  id: number
  user_id: number
  plan_id: number
  starts_at: string
  expires_at: string
  canceled: boolean
  traffic_used_bytes: number
  traffic_bonus_bytes: number
  traffic_limit_bytes: number | null
  created_at: string
  plan_name: string
  state: PeriodState
  payment_id: string | null
}

export interface Device {
  id: number
  user_agent: string
  os: string
  model: string
  first_seen: string
  last_seen: string
}

export type PaymentStatus = "pending" | "succeeded" | "canceled" | "mismatch" | "rejected" | "refunded"

export interface Payment {
  id: number
  payment_id: string
  user_id: number
  plan_id: number
  amount_rub: number
  status: PaymentStatus
  source: "yookassa" | "manual" | string
  paid_at: string | null
  period_id: number | null
  note: string | null
  refunded_at: string | null
  created_at: string
  telegram_id: number | null
  plan_name: string
  confirmation_url?: string | null
}

export interface PaymentsPage {
  payments: Payment[]
  next_before: number | null
}

export interface TelegramInfo {
  username: string | null
  name: string
  updated_at: string
}

export interface UserCard {
  user: User
  telegram: TelegramInfo | null
  periods: Period[]
  devices: Device[]
  trial: { plan_id: number; period_id: number; granted_at: string } | null
  access: { plan_id: number; plan_name: string; source: string; granted_at: string; revoked_at: string | null }[]
  payments: Payment[]
}

export interface UserStats {
  period_id: number | null
  nodes: { node_id: number; name: string; today: number; days_7: number; days_30: number; period: number }[]
}

export interface Node {
  id: number
  cert_cn: string
  name: string
  group_id: number | null
  address: string
  users_version: number
  enabled: boolean
  config_error: string | null
  last_heartbeat: string | null
  agent_version: string | null
  xray_version: string | null
  active_users: number | null
  agent_uptime_sec: number | null
  xray_uptime_sec: number | null
  last_sync_at: string | null
  status: NodeStatus
  inbounds: number
  created_at: string
}

export interface Inbound {
  id: number
  node_id: number
  tag: string
  port: number
  protocol: string
  transport: string
  security: string
  flow: string
  label: string
  reality: {
    dest: string
    server_names: string[]
    short_ids: string[]
    fingerprint: string
    public_key: string
  }
  created_at: string
}

export interface Group {
  id: number
  name: string
  created_at: string
}

export interface Plan {
  id: number
  name: string
  price_rub: number
  period_days: number
  traffic_limit_bytes: number | null
  location_group_id: number | null
  device_limit: number | null
  on_sale: boolean
  hwid_required: boolean
  trial: boolean
  visibility: "public" | "private"
  created_at: string
  active_users?: number
}

export interface UpDown {
  up: number
  down: number
}

export interface TopUser {
  user_id: number
  telegram_id: number | null
  telegram_username: string | null
  telegram_name: string | null
  bytes: number
}

export interface NodeTraffic {
  today: UpDown
  days_7: UpDown
  days_30: UpDown
  daily: (UpDown & { day: string })[]
  top: { today: TopUser[]; days_7: TopUser[]; days_30: TopUser[] }
}

export interface Settings {
  subs_title: string
  subs_support_url: string
  updated_at: string
}

export interface Invite {
  id: number
  code: string
  plan_id: number
  max_uses: number | null
  uses: number
  expires_at: string | null
  revoked_at: string | null
  created_at: string
  link: string | null
}

export interface Access {
  plan_id: number
  user_id: number
  telegram_id: number | null
  source: "invite" | "admin" | string
  invite_id: number | null
  granted_at: string
  revoked_at: string | null
}

export interface AuditEntry {
  id: number
  at: string
  peer_cn: string
  operator: string
  method: string
  route: string
  path: string
  entity: string
  entity_id: string
  action: string
  details: Record<string, unknown> | null
}

export interface AuditPage {
  entries: AuditEntry[]
  next_before: number | null
}

export interface PaymentActionResult {
  status: PaymentStatus
  applied: boolean
  period_id?: number
  period_closed: boolean
  provider?: { found: boolean; status?: string; amount?: string; refunded?: string }
}
