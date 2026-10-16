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
