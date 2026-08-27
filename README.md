# Unique Clicks API

[![CI](https://github.com/niksmi-lab/unique-clicks-service/actions/workflows/ci.yml/badge.svg)](https://github.com/niksmi-lab/unique-clicks-service/actions/workflows/ci.yml)

Production-style микросервис для учёта уникальных кликов по авторам. Уникальность определяется тройкой **UTC-день + author_id + user_id**: повторный клик того же пользователя по тому же автору в течение дня не увеличивает метрику.

## Быстрый запуск

Нужны Docker и Docker Compose:

```bash
docker compose up --build
```

PostgreSQL поднимется на `localhost:5433`, API — на `localhost:8080`, Prometheus UI — на `localhost:9090`. Миграция из `migrations/` применяется при первом создании Docker volume.

```bash
curl -i -X POST http://localhost:8080/v1/clicks \
  -H 'Content-Type: application/json' \
  -d '{"user_id":101,"author_id":42}'

curl -s -X POST http://localhost:8080/v1/metrics/yesterday \
  -H 'Content-Type: application/json' \
  -d '{"author_ids":[42,43]}'
```

## API

Полный контракт находится в [api/openapi.yaml](api/openapi.yaml).

- `POST /v1/clicks` сохраняет клик и возвращает `202 Accepted`. Операция идемпотентна в пределах UTC-дня.
- `POST /v1/metrics/yesterday` возвращает число уникальных пользователей за предыдущий UTC-день. Для авторов без кликов возвращается `0`.
- `GET /health/live` проверяет, что процесс работает.
- `GET /health/ready` проверяет, что процесс видит PostgreSQL.
- `GET /metrics` отдаёт Prometheus exposition format.

Старые маршруты `POST /click` и `POST /author-metrics` сохранены как compatibility aliases. Новым клиентам следует использовать версионированные URL.

Тела запросов должны иметь `Content-Type: application/json`. Неизвестные поля, лишние JSON-объекты, неположительные идентификаторы и более 1000 авторов отклоняются с `400`. Размер тела ограничен 1 MiB.

## Архитектура

```text
cmd/main.go                  composition root, lifecycle приложения
internal/config              конфигурация из environment variables
internal/handlers            HTTP-контракт, decode/validate, health checks
internal/httpmw              request ID, access log, panic recovery, headers
internal/metrics             private Prometheus registry и collectors
internal/service             бизнес-правила
internal/storage/postgres    PostgreSQL repository
internal/worker              управляемая retention-очистка
deployments/prometheus       scrape config и alert rules
internal/models              доменные структуры
migrations                   версия схемы БД
api/openapi.yaml             машиночитаемый HTTP-контракт
```

Зависимости направлены внутрь: бизнес-сервис знает только интерфейс хранилища, а HTTP-слой — интерфейс бизнес-сервиса. Поэтому правила уникальности и расчёта даты тестируются без настоящей БД.

## Конфигурация

| Переменная | Default | Назначение |
|---|---:|---|
| `APP_ENV` | `development` | Окружение и уровень логирования |
| `SERVER_ADDR` | `:8080` | Адрес HTTP-сервера |
| `DATABASE_URL` | локальный PostgreSQL | DSN; в production задаётся secret-ом |
| `REQUEST_TIMEOUT` | `5s` | Лимит бизнес-операции |
| `SHUTDOWN_TIMEOUT` | `10s` | Время на graceful shutdown |
| `CLEANUP_INTERVAL` | `1h` | Частота retention job |
| `RETENTION_DAYS` | `30` | Сколько UTC-дней хранить |
| `DB_MAX_CONNECTIONS` | `10` | Максимум соединений pgx pool |

Шаблон значений находится в [.env.example](.env.example).

## Мониторинг Prometheus

Сервис использует отдельный registry и экспортирует:

- RED-метрики HTTP: `unique_clicks_http_requests_total`, `unique_clicks_http_request_duration_seconds`, `unique_clicks_http_requests_in_flight`;
- бизнес-метрики: `unique_clicks_clicks_total{result}` и `unique_clicks_metrics_queries_total{result}`;
- PostgreSQL: количество и длительность операций, acquired/idle/total/max connections, ожидания и закрытия соединений pool;
- retention worker: число запусков, ошибки, длительность, удалённые строки и время последнего успеха;
- стандартные `go_*`, `process_*`, состояние scrape handler и `unique_clicks_build_info`.

Labels ограничены фиксированными наборами `method`, route pattern, HTTP status, operation и result. `user_id` и `author_id` намеренно не экспортируются: идентификаторы создали бы неограниченную cardinality.

В [deployments/prometheus/alerts.yml](deployments/prometheus/alerts.yml) определены алерты на:

- недоступность сервиса;
- долю HTTP 5xx выше 5%;
- p95 latency выше одной секунды;
- насыщение PostgreSQL pool;
- ошибки storage и click ingestion;
- ошибку или отсутствие успешного retention cleanup.

Prometheus автоматически собирает `/metrics` каждые 15 секунд. В production конфигурацию scrape и alert rules обычно переносят в общий observability stack, а `/metrics` закрывают от публичного доступа network policy или internal ingress.

## Что делает реализацию продуктовой

- Уникальность гарантирует primary key в PostgreSQL, а `ON CONFLICT DO NOTHING` делает повторную доставку события безопасной.
- Ошибки записи и чтения доходят до HTTP-слоя; внутренняя ошибка больше не маскируется под `404`.
- Все календарные границы считаются в UTC.
- Есть строгий JSON decoder, валидация, лимиты тела и времени.
- JSON-логи содержат request ID, статус, latency и размер ответа.
- Фоновый worker привязан к lifecycle приложения и останавливается по `SIGINT/SIGTERM`.
- Retention удаляет данные старше настраиваемого периода, а не всё старше вчерашнего дня.
- Graceful shutdown завершает активные HTTP-запросы и закрывает pool.
- Prometheus даёт RED-, business-, database- и background-job observability с готовыми alert rules.

Для очень большого потока непосредственная запись в PostgreSQL станет узким местом. Следующий этап масштабирования — очередь событий, партиционирование таблицы по дате и асинхронная агрегация. Для текущего масштаба прямой idempotent insert проще и надёжнее.

## Разработка

```bash
make test
make test-race
make vet
make build
make prometheus-check # если локально установлен promtool
```

Для запуска API без Docker потребуется PostgreSQL и применённая миграция:

```bash
psql "$DATABASE_URL" -f migrations/000001_create_clicks.up.sql
go run ./cmd
```
