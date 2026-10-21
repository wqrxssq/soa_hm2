# Marketplace core

Один сервис `core`: каталог, заказы, промокоды, пользователи и роли. Одна PostgreSQL, локальная транзакция на резерв остатка и создание заказа.

Стек: Go, OpenAPI, PostgreSQL, Flyway, JWT.

## Запуск

```bash
docker compose up --build
```

Сервис слушает `http://localhost:8080`. База доступна на `localhost:5432` (`marketplace` / `marketplace`, база `marketplace`).

Проверка:

```bash
curl -i http://localhost:8080/health
```

Остановка с удалением данных:

```bash
docker compose down -v
```

Таблицы на защите:

```bash
docker compose exec postgres psql -U marketplace -d marketplace
```

```sql
SELECT id, name, stock, status FROM products;
SELECT id, user_id, status, total_amount, discount_amount FROM orders;
SELECT * FROM order_items;
SELECT code, current_uses FROM promo_codes;
```

Кодогенерация одной командой (её же выполняет сборка образа):

```bash
make generate
```

Сгенерированный `services/core/internal/api/openapi.gen.go` лежит в `.gitignore`.

## Контракт

Спецификация: `services/core/api/openapi/openapi.yaml`.

Модели и интерфейс хэндлеров генерируются из неё. Ограничения вроде длины, `minimum` и `pattern` проверяются по спецификации до бизнес-логики. Ошибка валидации — `VALIDATION_ERROR`, поля и причины в `details.fields`.

Роль передаётся в `POST /auth/register` (`USER`, `SELLER`, `ADMIN`), чтобы на защите можно было завести все три. Access token живёт 15 минут, refresh — 7 дней. В access token есть claim `role`. Повторный `POST /auth/refresh` со старым refresh token не проходит: токен поворачивается.

`POST /orders/{id}/advance` переводит заказ ровно на один шаг: `CREATED → PAYMENT_PENDING → PAID → SHIPPED → COMPLETED`. Отмена — `POST /orders/{id}/cancel`, только из `CREATED` и `PAYMENT_PENDING`.

Процентная скидка строго больше 70 не применяется: покупатель платит полную сумму. Так читается формулировка «не более 70% от total_amount, в ином случае цена 100%».

Лимит частоты создания и обновления заказа задаётся `ORDER_RATE_LIMIT` (по умолчанию `1m`). Значение `0s` выключает проверку.

Каждый запрос пишет одну JSON-строку в stdout и возвращает `X-Request-Id`. В теле мутирующих запросов пароль и токены маскируются.
