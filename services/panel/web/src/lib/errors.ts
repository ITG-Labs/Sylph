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
  user_id: "Пользователь заблокирован. Сначала разблокируйте его.",
  subscription: "У пользователя нет активного периода.",
  bytes: "У выбранного пула нет лимита трафика.",
  status: "Статус платежа уже изменился. Обновите страницу.",
  source: "Ручной платёж нельзя перепроверить в ЮKassa.",
  id: "Период уже закрыт или закончился.",
  trial: "В продаже уже есть пробный тариф. Сначала снимите его с продажи.",
  name: "Это имя уже занято.",
  "reality.server_names": "Этот SNI уже используется другим входом этой ноды.",
  tag: "Этот тег уже используется на этой ноде.",
  cert_cn: "Нода с этим CN уже добавлена.",
  payment_id: "Этот идентификатор платежа уже занят.",
  group: "Группа используется нодами или тарифами. Сначала уберите их из группы.",
}

const byCode: Record<string, string> = {
  provider_unavailable: "ЮKassa не отвечает. Повторите попытку позже.",
  payments_disabled: "Приём платежей через ЮKassa не настроен в ядре.",
  core_unavailable: "Ядро не отвечает.",
  core_rejected: "Ядро отклонило сертификат панели. Проверьте, не отозван ли он.",
  not_found: "Не найдено.",
  too_many_attempts: "Слишком много попыток входа. Повторите через 15 минут.",
  busy: "Панель перегружена попытками входа. Повторите через минуту.",
  bad_credentials: "Неверное имя, пароль или код.",
  unauthorized: "Сеанс завершён. Войдите снова.",
  csrf: "Запрос отклонён защитой от подделки запросов. Обновите страницу.",
  too_large: "Слишком большой запрос.",
  banned: "Пользователь заблокирован.",
  internal: "Внутренняя ошибка ядра. Подробности — в журнале ядра.",
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
  cert_cn: "CN сертификата",
  name: "Имя",
  price_rub: "Цена",
  period_days: "Срок",
  pools: "Пулы",
  pool_id: "Пул",
  inbound: "Вход",
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
    if (err.status === 409 && err.field === "status" && err.message.includes("still waiting")) return "С момента создания платежа не прошло суток: пользователь ещё может его оплатить."
    if (err.status === 409 && byField[err.field]) return byField[err.field]
    if (byCode[err.code]) return byCode[err.code]
    if (err.field) return `${fieldLabels[err.field] ?? err.field}: ${err.message}`
    return err.message || `Ошибка ${err.status}`
  }
  if (err instanceof Error) return err.message
  return String(err)
}
