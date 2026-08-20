# AiR_Payment

![air_payment](air_payment_logo.png)

[🇷🇺 Русская версия](README.ru.md)

A microservice of the AiR platform for creating and tracking cryptocurrency payments through Bybit.

![Go version](https://img.shields.io/badge/Go-1.25.8-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
[![Telegram](https://img.shields.io/badge/Telegram-Join%20Chat-blue?logo=telegram)](https://t.me/marusia_dev)

The service provides an HTTP API for payments, donations, tariff retrieval, and currency listing. Payment statuses can be retrieved with a regular request or in real time through Server-Sent Events (SSE).

## Features

- creating cryptocurrency payments through Bybit;
- retrieving payment statuses;
- streaming status updates through SSE;
- retrieving user tariffs and supported currencies;
- retrieving donation addresses;
- support for Bybit mainnet and testnet;
- MySQL for data storage;
- Prometheus metrics and structured logging.

## Architecture

```text
AiR platform
      |
      | HTTP/JSON, X-User-ID
      v
  air_payment (:8080)
      |             |
      v             v
   MySQL        Bybit API
```

For user-specific routes, the service reads the user ID from the `X-User-ID` header. Payment data is stored in MySQL, while cryptocurrency operations are performed through the Bybit API.

## HTTP API

The service listens on port `8080`. The complete specification is available in [`doc/openapi.yaml`](doc/openapi.yaml).

| Method | Route | Purpose | Authentication |
|---|---|---|---|
| `GET` | `/metrics` | Prometheus metrics | none |
| `GET` | `/pay/available` | Service availability check | none |
| `GET` | `/pay/tariff` | User tariff | `X-User-ID` |
| `GET` | `/pay/currencies` | Supported currencies | `X-User-ID` |
| `GET` | `/pay/donation-currencies` | Currencies for donations | none |
| `POST` | `/pay/create-payment` | Create a payment | none |
| `POST` | `/pay/donation-address` | Get a donation address | none |
| `GET` | `/pay/payment-status` | Payment status | `X-User-ID` |
| `GET` | `/pay/payment-status-stream` | SSE status stream | `X-User-ID` |

## Running

Docker Compose is used for local development:

```bash
docker compose -f dev.yml up --build
```

To stop the service:

```bash
docker compose -f dev.yml down
```

The external Docker networks `air_shared` and `monitoring_shared` are required. A MySQL container named `air_db` must be available on the `air_shared` network.

Before starting the service, create the key files:

```text
secrets/api_key.txt
secrets/secret_key.txt
```

These files must contain the Bybit API key and secret key respectively. Do not commit them to Git.

## Configuration

Environment variables used by Compose and the application:

```text
DB_HOST=air_db:3306
DB_NAME=air
DB_USER=<MySQL user>
DB_PASSWORD=<MySQL password>
TESTNET=false
API_KEY_FILE=/run/secrets/api_key
SECRET_KEY_FILE=/run/secrets/secret_key
REAL_URL=<service domain>
LOG_LEVEL=info
```

`dev.yml` uses `REAL_URL=localhost` and the `debug` log level by default. In `prod.yml`, the domain is provided through the `DOMAIN` variable and the log level is set to `info`.

## Production deployment

The [`.github/workflows/deploy.yml`](.github/workflows/deploy.yml) workflow runs when changes are pushed to the `master` branch and performs the following steps:

1. determines a new version from the latest Git tag and the commit message;
2. creates and pushes a Git tag;
3. builds the Docker image and publishes it to GHCR with `prod` and version tags;
4. copies `prod.yml` and the version file to the server;
5. creates the secrets and `.env`, pulls the image, and restarts Compose.

The workflow requires the secrets `HOST`, `USERNAME`, `SSH_PRIVATE_KEY`, `GITHUB_TOKEN`, `API_KEY_FILE`, `SECRET_KEY_FILE`, `DB_USER`, and `DB_PASSWORD`. The application is deployed to `/bot/marusia/air_payment` on the server.

To start the production Compose setup manually after preparing `.env` and `secrets/`:

```bash
docker compose -f prod.yml up -d --build
```

## Development and tests

The project requires Go `1.25.8`.

```bash
go test ./...
go build ./cmd/...
```

## Project structure

- `cmd/main.go` — entry point;
- `internal/app` — application startup and graceful shutdown;
- `internal/delivery/http` — HTTP API, middleware, and SSE;
- `internal/processor/bybit` — Bybit integration and payment processing;
- `internal/repository/mysql` — MySQL repository;
- `internal/db` — database connection;
- `internal/metrics` — Prometheus metrics;
- `doc/openapi.yaml` — API specification;
- `dev.yml`, `prod.yml` — Docker Compose configurations.

## Technologies

Go, Fiber, MySQL, Bybit API, Docker, Docker Compose, Prometheus, and OpenAPI 3.0.3.

## License

The project is distributed under the [MIT](LICENSE) license.

## Contacts

[![Telegram](https://img.shields.io/badge/Telegram-Contact-blue?logo=telegram)](https://t.me/ikermy)
