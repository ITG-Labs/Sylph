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

## Сертификаты

    make ca-init
    make cert NAME=bot TYPE=client
    make cert NAME=core TYPE=server
    make revoke NAME=bot

Каталог ca/ содержит приватный ключ корневого сертификата и не попадает в git.
