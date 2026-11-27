# mytonstorage-backend

**[English version](README.md)**

Backend-сервис для mytonstorage.org.

## Описание

Backend для загрузки и управления файлами в TON Storage:
- Загрузка файлов в TON Storage
- Управление жизненным циклом storage-контрактов (инициализация, пополнение, закрытие, обновление провайдеров)
- Авторизация через TON Connect
- Мониторит storage-контракты и уведомляет провайдеров о новых bags для загрузки
- REST API эндпоинты для фронтенд-приложения
- Собирает метрики через **Prometheus**

## Локальная разработка (Docker)

**Нужно:** [Docker](https://docs.docker.com/), [Task](https://taskfile.dev), Go 1.26+ (для локальной сборки вне контейнера).

Стек в контейнерах: **PostgreSQL**, **tonutils-storage** (v1.5.1), **backend**.

```bash
# Поднять стек (создаст deploy/.env при первом запуске)
task deploy:up

# Проверить health
task deploy:health

# Логи
task deploy:logs

# Остановить
task deploy:down

# Остановить и удалить volumes (БД + bags)
task deploy:reset
```

API: `http://localhost:9092` (порт задаётся `BACKEND_PORT` в `deploy/.env`).

Конфиг: скопируй `deploy/.env.example` → `deploy/.env` или запусти `task deploy:init`.  
`DB_USER` должен быть **pguser** — так задано в `db/init.sql`.

### Фронтенд (локально)

[mytonstorage-frontend](https://github.com/mytonprovider/mytonstorage-frontend): его dev-сервер проксирует `/api`, на этот backend его направляет `BACKEND_PROXY_TARGET=http://localhost:9092`.
Настройка и TON Connect — в его README.

## Деплой на VPS

Образ публикует задание `publish` в `CI` после проверок при каждом push в `master`: `ghcr.io/mytonprovider/mytonstorage-backend:<tag>`, тег — первые 7 символов коммита. Ручной `task image:build:push` остаётся запасным путём.

Образ tonutils-storage здесь не собирается: `TONUTILS_STORAGE_IMAGE=ghcr.io/mytonprovider/tonutils-storage:v1.5.1`.

Для release-образа backend не задавай `BUILD_TAGS` (или `BUILD_TAGS=`).

На VPS (нужны `deploy/docker-compose.hub.yml`, `deploy/.env.hub`, `deploy/secrets/agents-ca.crt`):

```bash
task hub:init
nano deploy/.env.hub
task hub:up
task hub:health
```

В `.env.hub` задай `BACKEND_IMAGE`, `TONUTILS_STORAGE_IMAGE`, `SYSTEM_HOST=mytonstorage.org`, `AGENT_*`, `TONUTILS_STORAGE_EXTERNAL_IP` (публичный IP/DNS хоста).

Фронтенд — отдельно: [mytonstorage-frontend](https://github.com/mytonprovider/mytonstorage-frontend).

Подробнее: [deploy/README.ru.md](deploy/README.ru.md).

## Локальный запуск без Docker

```bash
task build          # bin/mtpo-backend
task test:build     # проверка compile
```

Для полного flow нужны Postgres и tonutils-storage — проще через `task deploy:up`.

## Структура проекта

```
├── cmd/                   # Точка входа, конфиг, инициализация
├── pkg/                   # Пакеты приложения
│   ├── cache/
│   ├── clients/           # TON blockchain и TON Storage HTTP
│   ├── httpServer/
│   ├── models/
│   ├── repositories/
│   ├── services/
│   └── workers/
├── db/                    # Схема PostgreSQL
├── deploy/                # Docker Compose, .env.example
├── Dockerfile
└── Taskfile.yml
```

## API эндпоинты

Подробное описание всех ручек: [docs/API.ru.md](docs/API.ru.md)

- Логин через TON Connect
- Файлы: загрузка, удаление, unpaid bags, краткая инфа
- Контракты: init, topup, withdraw, смена провайдеров
- Провайдеры: offers (тарифы)

## Воркеры

- **Files Worker**: unpaid/expired bags, уведомление провайдеров, проверка загрузки
- **Cleaner Worker**: очистка устаревших данных в БД

## Лицензия

Apache-2.0

Проект создан по заказу участника сообщества TON Foundation.
