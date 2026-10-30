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
  group: "В группе ещё есть ноды или тарифы — сначала перенеси их",
}

const byCode: Record<string, string> = {
  provider_unavailable: "ЮKassa не ответила — попробуй позже",
  payments_disabled: "ЮKassa в ядре не настроена",
  core_unavailable: "Ядро не отвечает",
  core_rejected: "Ядро отклонило серт панели (отозван?)",
  not_found: "Не найдено",
  too_many_attempts: "Слишком много попыток — подожди 15 минут",
  busy: "Панель сейчас перегружена входами — попробуй через минуту",
  bad_credentials: "Неверное имя, пароль или код",
  unauthorized: "Сессия закончилась — войди заново",
  csrf: "Запрос отклонён защитой от подделки — обнови страницу",
  too_large: "Слишком большой запрос",
  banned: "Юзер забанен",
  internal: "Ошибка ядра — смотри логи",
}

const fieldLabels: Record<string, string> = {
  "reality.dest": "dest",
  "reality.server_names": "SNI",
  "reality.short_ids": "shortId",
  "reality.fingerprint": "Отпечаток",
  "reality.private_key": "Приватный ключ",
  transport: "Транспорт",
  xhttp: "XHTTP",
  "xhttp.path": "Путь",
  "xhttp.mode": "mode сервера",
  "xhttp.client_mode": "mode клиента",
  tls: "TLS",
  "tls.domain": "Домен ноды",
  "tls.decoy": "Маскировка",
  "tls.address": "Адрес для клиентов",
  "tls.port": "Порт для клиентов",
  "tls.sni": "SNI для клиентов",
  "tls.host": "Host",
  "tls.alpn": "ALPN",
  "tls.fingerprint": "Отпечаток",
  tag: "Тег",
  label: "Подпись",
  port: "Порт",
  address: "Адрес",
  cert_cn: "CN серта",
  name: "Имя",
  price_rub: "Цена",
  period_days: "Срок",
  traffic_limit_bytes: "Лимит трафика",
  device_limit: "Лимит устройств",
  hwid_required: "HWID",
  max_uses: "Сколько раз",
  expires_at: "Срок действия",
  reason: "Причина",
  days: "Дни",
  bytes: "Трафик",
  telegram_id: "Telegram ID",
  subs_title: "Название",
  subs_support_url: "Ссылка поддержки",
  settings: "Настройки",
  terms_text: "Текст соглашения",
  terms_url: "Ссылка на соглашение",
  channel_chat: "Канал",
  channel_url: "Ссылка на канал",
  channel_text: "Текст про канал",
  help_text: "Текст помощи",
  help_buttons: "Кнопки помощи",
  q: "Поиск",
}

export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409 && err.field === "status" && err.message.includes("still waiting")) return "Платёж моложе суток — юзер ещё может оплатить, отклонять рано"
    if (err.status === 409 && byField[err.field]) return byField[err.field]
    if (byCode[err.code]) return byCode[err.code]
    if (err.field) return `${fieldLabels[err.field] ?? err.field}: ${err.message}`
    return err.message || `Ошибка ${err.status}`
  }
  if (err instanceof Error) return err.message
  return String(err)
}
