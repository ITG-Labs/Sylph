# Sylph

Микросервисная панель управления для VLESS: ядро, агент ноды, админ-панель,
Telegram-бот, подписки и планировщик — по сервису на машину.

## Структура

- pkg/logging — JSON-логи и сквозной X-Request-Id
- pkg/mtls — мини-CA и mTLS-конфиги
- pkg/apiclient — клиент API ядра
- tools/sylphca — выпуск и отзыв сертификатов
- internal/version — версия для ldflags

## Разработка

    make test
    make vet
    make lint
    make test-db
    make test-xray
    make proto

test-db и test-xray поднимают в docker Postgres и xray, прогоняют тесты с -race и гасят
контейнеры. proto перегенерирует Go-код клиента xray из services/agent/xrayproto.

## Сертификаты

    make ca-init
    make cert NAME=bot TYPE=client ROLE=bot
    make cert NAME=node-1 TYPE=client ROLE=node
    make cert NAME=core.sylph.internal TYPE=server
    make revoke NAME=bot

Роль клиента (bot, subs, cron, panel, node) пишется в OU сертификата, ядро
пускает к эндпоинтам только по ней. Серверный сертификат выпускается без роли.

Имя серверного сертификата — DNS-имя, которое проверяют клиенты (агент по умолчанию ждёт
core.sylph.internal, переопределяется SYLPH_CORE_NAME).

Каталог ca/ содержит приватный ключ корневого сертификата и не попадает в git.

## Деплой

Образы собирает CI после зелёных тестов и публикует в GHCR:
ghcr.io/itg-labs/sylph-{core,cron,subs,bot,panel,agent,caddy}. Пуш в main даёт теги edge и
sha-<коммит>, git-тег vX.Y.Z — тег X.Y.Z. В проде указывайте конкретную версию.

Каждой машине — своя папка из deploy/prod:

- core — ядро, Postgres и крон;
- panel — панель за Caddy;
- bot — бот, наружу через Caddy только вебхук ЮKassa;
- subs — подписки за Caddy;
- node — нода: Caddy с layer4, xray и агент.

Папку роли кладут на машину, рядом создают .env по .env.example и certs/ с сертификатами (панели
ещё нужен admins.json от sylph-panel new-admin), затем docker compose up -d. Всё состояние роли
лежит в её папке: база, кэш и учёт трафика агента, geo-базы, сертификаты Caddy. Чтобы перенести
машину, остановите compose, перенесите папку и поднимите на новом месте: владельцев и права на
data/ и certs/ при каждом запуске выставляет служебный контейнер perms.

Сертификаты в certs/:

- core: ca.pem, revoked.json (если что-то отозвано), core/cert.pem и core/key.pem (серверный
  core.sylph.internal), cron/cert.pem и cron/key.pem;
- panel, bot, subs, node: ca.pem, cert.pem и key.pem своей роли.

Порядок выкатки: ядро, ноды, панель, затем бот и subs.
