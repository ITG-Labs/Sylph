export class ApiError extends Error {
  status: number
  code: string
  field: string

  constructor(status: number, code: string, field: string, message: string) {
    super(message)
    this.status = status
    this.code = code
    this.field = field
  }
}

const byField: Record<string, string> = {
  user_id: "Юзер забанен — сначала разбань",
  subscription: "У юзера нет активного периода",
  bytes: "Тариф без лимита — докидывать трафик некуда",
  status: "Платёж уже в другом статусе — обнови страницу",
  source: "Ручной платёж в ЮKassa не перепроверить",
  id: "Период уже закрыт или закончился",
  trial: "Пробный тариф в продаже уже есть — сначала сними старый",
  name: "Такое имя уже занято",
  "reality.server_names": "Такой SNI уже есть у другого входа этой ноды",
  tag: "Такой тег уже есть у этой ноды",
  cert_cn: "Нода с таким CN уже заведена",
  payment_id: "Такой id платежа уже занят",
}

const byCode: Record<string, string> = {
  provider_unavailable: "ЮKassa не ответила — попробуй позже",
  payments_disabled: "ЮKassa в ядре не настроена",
  core_unavailable: "Ядро не отвечает",
  core_rejected: "Ядро отклонило серт панели (отозван?)",
  not_found: "Не найдено",
  too_many_attempts: "Слишком много попыток — подожди 15 минут",
  bad_credentials: "Неверное имя, пароль или код",
  unauthorized: "Сессия закончилась — войди заново",
  csrf: "Запрос отклонён защитой от подделки — обнови страницу",
  too_large: "Слишком большой запрос",
  banned: "Юзер забанен",
  internal: "Ошибка ядра — смотри логи",
}

export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409 && err.field === "status" && err.message.includes("still waiting")) return "Платёж моложе суток — юзер ещё может оплатить, отклонять рано"
    if (err.status === 409 && byField[err.field]) return byField[err.field]
    if (byCode[err.code]) return byCode[err.code]
    if (err.field) return `${err.field}: ${err.message}`
    return err.message || `Ошибка ${err.status}`
  }
  if (err instanceof Error) return err.message
  return String(err)
}
