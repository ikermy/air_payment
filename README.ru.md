# AiR_Payment

![air_payment](air_payment_logo.png)

[🇬🇧 English version](README.md)

Микросервис платформы AiR для создания и отслеживания криптовалютных платежей через Bybit.

![Версия Go](https://img.shields.io/badge/Go-1.25.8-00ADD8?logo=go)
![Лицензия](https://img.shields.io/badge/license-MIT-blue)
[![Telegram](https://img.shields.io/badge/Telegram-Join%20Chat-blue?logo=telegram)](https://t.me/marusia_dev)

Сервис предоставляет HTTP API для платежей, пожертвований, получения тарифа и списка валют. Состояние платежей можно получать обычным запросом или в реальном времени через Server-Sent Events (SSE).

## Возможности

- создание криптовалютного платежа через Bybit;
- получение статуса платежа;
- потоковые уведомления об изменении статуса через SSE;
- получение тарифа и поддерживаемых валют;
- получение адреса для пожертвований;
- работа с Bybit mainnet и testnet;
- MySQL для хранения данных;
- Prometheus-метрики и структурированные логи.

## Архитектура

```text
Платформа AiR
      |
      | HTTP/JSON, X-User-ID
      v
  air_payment (:8080)
      |             |
      v             v
   MySQL        Bybit API
```

Для пользовательских маршрутов сервис получает идентификатор пользователя из заголовка `X-User-ID`. Данные платежей хранятся в MySQL, а операции с криптовалютой выполняются через API Bybit.

## HTTP API

Сервис слушает порт `8080`. Полная спецификация находится в [`doc/openapi.yaml`](doc/openapi.yaml).

| Метод | Маршрут | Назначение | Авторизация |
|---|---|---|---|
| `GET` | `/metrics` | Prometheus-метрики | нет |
| `GET` | `/pay/available` | Проверка доступности сервиса | нет |
| `GET` | `/pay/tariff` | Тариф пользователя | `X-User-ID` |
| `GET` | `/pay/currencies` | Поддерживаемые валюты | `X-User-ID` |
| `GET` | `/pay/donation-currencies` | Валюты для пожертвований | нет |
| `POST` | `/pay/create-payment` | Создание платежа | нет |
| `POST` | `/pay/donation-address` | Адрес для пожертвования | нет |
| `GET` | `/pay/payment-status` | Статус платежа | `X-User-ID` |
| `GET` | `/pay/payment-status-stream` | SSE-поток статусов | `X-User-ID` |

## Запуск

Для локальной разработки используется Docker Compose:

```bash
docker compose -f dev.yml up --build
```

Остановка:

```bash
docker compose -f dev.yml down
```

Для запуска необходимы внешние Docker-сети `air_shared` и `monitoring_shared`. В сети `air_shared` должен быть доступен MySQL-контейнер `air_db`.

Перед запуском создайте файлы ключей:

```text
secrets/api_key.txt
secrets/secret_key.txt
```

Файлы должны содержать API key и secret key Bybit соответственно. Не добавляйте их в Git.

## Конфигурация

Переменные окружения, используемые Compose и приложением:

```text
DB_HOST=air_db:3306
DB_NAME=air
DB_USER=<пользователь MySQL>
DB_PASSWORD=<пароль MySQL>
TESTNET=false
API_KEY_FILE=/run/secrets/api_key
SECRET_KEY_FILE=/run/secrets/secret_key
REAL_URL=<домен сервиса>
LOG_LEVEL=info
```

В `dev.yml` по умолчанию используется `REAL_URL=localhost` и уровень логирования `debug`. В `prod.yml` домен передаётся через переменную `DOMAIN`, а уровень логирования установлен в `info`.

## Production-деплой

Workflow [`.github/workflows/deploy.yml`](.github/workflows/deploy.yml) запускается при push в ветку `master` и выполняет следующие действия:

1. определяет новую версию по последнему Git-тегу и сообщению коммита;
2. создаёт и публикует Git-тег;
3. собирает Docker-образ и публикует его в GHCR с тегами `prod` и версии;
4. копирует `prod.yml` и версию на сервер;
5. создаёт секреты и `.env`, загружает образ и перезапускает Compose.

Для workflow требуются secrets `HOST`, `USERNAME`, `SSH_PRIVATE_KEY`, `GITHUB_TOKEN`, `API_KEY_FILE`, `SECRET_KEY_FILE`, `DB_USER` и `DB_PASSWORD`. На сервере приложение размещается в `/bot/marusia/air_payment`.

Ручной запуск production Compose после подготовки `.env` и `secrets/`:

```bash
docker compose -f prod.yml up -d --build
```

## Разработка и тесты

Проект требует Go `1.25.8`.

```bash
go test ./...
go build ./cmd/...
```

## Структура проекта

- `cmd/main.go` — точка входа;
- `internal/app` — запуск и корректное завершение приложения;
- `internal/delivery/http` — HTTP API, middleware и SSE;
- `internal/processor/bybit` — интеграция с Bybit и обработка платежей;
- `internal/repository/mysql` — репозиторий MySQL;
- `internal/db` — подключение к базе данных;
- `internal/metrics` — Prometheus-метрики;
- `doc/openapi.yaml` — спецификация API;
- `dev.yml`, `prod.yml` — Docker Compose-конфигурации.

## Технологии

Go, Fiber, MySQL, Bybit API, Docker, Docker Compose, Prometheus и OpenAPI 3.0.3.

## Лицензия

Проект распространяется по лицензии [MIT](LICENSE).

## Контакты

[![Telegram](https://img.shields.io/badge/Telegram-Contact-blue?logo=telegram)](https://t.me/ikermy)
