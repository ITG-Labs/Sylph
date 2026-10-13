const TZ = "Europe/Moscow"
const units = ["Б", "КБ", "МБ", "ГБ", "ТБ", "ПБ"]
const GiB = 1024 ** 3

const num1 = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 })
const rub = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 0 })
const dateFmt = new Intl.DateTimeFormat("ru-RU", { timeZone: TZ, day: "2-digit", month: "2-digit", year: "numeric" })
const dateTimeFmt = new Intl.DateTimeFormat("ru-RU", {
  timeZone: TZ,
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
})

function nbsp(s: string) {
  return s.replace(/[  ]/g, " ")
}

export function fmtBytes(n: number | null | undefined): string {
  if (n === null || n === undefined) return "∞"
  if (n < 1024) return `${n} Б`
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${nbsp(num1.format(v))} ${units[i]}`
}

export function fmtRub(n: number): string {
  return `${nbsp(rub.format(n))} ₽`
}

export function fmtDate(s: string | null | undefined): string {
  if (!s) return "—"
  return dateFmt.format(new Date(s))
}

export function fmtDateTime(s: string | null | undefined): string {
  if (!s) return "—"
  return nbsp(dateTimeFmt.format(new Date(s)))
}

export function fmtRelative(s: string | null | undefined, now: Date = new Date()): string {
  if (!s) return "никогда"
  const sec = Math.round((now.getTime() - new Date(s).getTime()) / 1000)
  if (sec < 60) return "только что"
  if (sec < 3600) return `${Math.floor(sec / 60)} мин назад`
  if (sec < 86400) return `${Math.floor(sec / 3600)} ч назад`
  return `${Math.floor(sec / 86400)} дн назад`
}

export function fmtUptime(sec: number | null | undefined): string {
  if (sec === null || sec === undefined) return "—"
  if (sec < 60) return `${sec} с`
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return h > 0 ? `${d} дн ${h} ч` : `${d} дн`
  if (h > 0) return m > 0 ? `${h} ч ${m} мин` : `${h} ч`
  return `${m} мин`
}

export function parseGB(s: string): number | null {
  const t = s.trim().replace(",", ".")
  if (!/^\d+(\.\d+)?$/.test(t)) return null
  const v = Number(t)
  if (!(v > 0)) return null
  return Math.round(v * GiB)
}

export function trafficPercent(used: number, limit: number | null): number | null {
  if (limit === null) return null
  if (limit <= 0) return 100
  return Math.min(100, Math.round((used / limit) * 100))
}

export function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10
  const m100 = n % 100
  if (m10 === 1 && m100 !== 11) return one
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few
  return many
}
