#!/usr/bin/env python3
"""Прогон сценариев core против уже поднятого docker compose."""

import json
import os
import subprocess
import time
import urllib.error
import urllib.request
import uuid

BASE = os.environ.get("BASE", "http://localhost:8080")
PASSWORD = "Sup3rSecret"


class ApiError(Exception):
    pass


def request(method, path, body=None, token=None, expected=200):
    data = None if body is None else json.dumps(body).encode()
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            raw = response.read()
            status = response.status
            payload = json.loads(raw) if raw else None
            header_id = response.headers.get("X-Request-Id")
    except urllib.error.HTTPError as err:
        raw = err.read()
        status = err.code
        payload = json.loads(raw) if raw else None
        header_id = err.headers.get("X-Request-Id")
    if status != expected:
        raise ApiError(f"{method} {path} -> {status}, want {expected}: {payload}")
    return payload, header_id


def wait_health():
    deadline = time.time() + 60
    last = None
    while time.time() < deadline:
        try:
            payload, _ = request("GET", "/health", expected=200)
            if payload.get("status") == "ok":
                return
        except Exception as err:  # noqa: BLE001 - ждём, пока compose поднимется
            last = err
        time.sleep(1)
    raise SystemExit(f"core не поднялся: {last}")


def register(role):
    email = f"{role.lower()}-{uuid.uuid4().hex[:8]}@example.com"
    user, _ = request(
        "POST",
        "/auth/register",
        {"email": email, "password": PASSWORD, "role": role},
        expected=201,
    )
    tokens, _ = request(
        "POST",
        "/auth/login",
        {"email": email, "password": PASSWORD},
        expected=200,
    )
    return user, tokens["access_token"], tokens["refresh_token"], email


def product_body(**overrides):
    body = {
        "name": "Book",
        "description": "A book",
        "price": 100,
        "stock": 50,
        "category": "books",
        "status": "ACTIVE",
    }
    body.update(overrides)
    return body


def stock_of(token, product_id):
    product, _ = request("GET", f"/products/{product_id}", token=token)
    return product["stock"]


def create_order(token, product_id, quantity, promo=None, expected=201):
    body = {"items": [{"product_id": product_id, "quantity": quantity}]}
    if promo is not None:
        body["promo_code"] = promo
    return request("POST", "/orders", body, token=token, expected=expected)


def main():
    wait_health()
    _, request_id = request("GET", "/health")
    if not request_id:
        raise SystemExit("нет X-Request-Id")

    seller, seller_token, _, seller_email = register("SELLER")
    _, buyer_token, buyer_refresh, buyer_email = register("USER")
    _, other_token, _, _ = register("USER")
    _, admin_token, _, _ = register("ADMIN")
    _, second_seller, _, _ = register("SELLER")

    request("POST", "/auth/login", {"email": buyer_email, "password": "wrong-pass"}, expected=401)
    request(
        "POST",
        "/auth/register",
        {"email": seller_email, "password": PASSWORD, "role": "SELLER"},
        expected=400,
    )
    request("GET", "/products", expected=401)
    request("POST", "/products", product_body(price=0), token=seller_token, expected=400)
    request("GET", "/products/not-a-uuid", token=buyer_token, expected=400)

    book, _ = request("POST", "/products", product_body(), token=seller_token, expected=201)
    book_id = book["id"]
    if book["seller_id"] != seller["id"] or book["status"] != "ACTIVE":
        raise SystemExit(f"неожиданный товар: {book}")
    request("POST", "/products", product_body(name="Denied"), token=buyer_token, expected=403)

    page, _ = request(
        "GET",
        "/products?page=0&size=20&status=ACTIVE&category=books",
        token=buyer_token,
    )
    if page["page"] != 0 or page["size"] != 20 or page["totalElements"] < 1:
        raise SystemExit(f"плохая страница: {page}")
    if not any(item["id"] == book_id for item in page["items"]):
        raise SystemExit("созданный товар не попал в список")

    before = stock_of(buyer_token, book_id)
    order, _ = create_order(buyer_token, book_id, 2)
    order_id = order["id"]
    if order["status"] != "CREATED" or order["total_amount"] != 200 or order["discount_amount"] != 0:
        raise SystemExit(f"неожиданный заказ: {order}")
    if order["items"][0]["price_at_order"] != 100 or order["items"][0]["quantity"] != 2:
        raise SystemExit(f"неожиданная позиция: {order['items']}")
    if stock_of(buyer_token, book_id) != before - 2:
        raise SystemExit("остаток не уменьшился")

    # Лимит частоты проверяется раньше активного заказа, поэтому повтор сразу даёт 429.
    limited_create, _ = create_order(buyer_token, book_id, 1, expected=429)
    if limited_create["error_code"] != "ORDER_LIMIT_EXCEEDED":
        raise SystemExit(limited_create)
    subprocess.check_call(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            "marketplace",
            "-d",
            "marketplace",
            "-c",
            f"UPDATE user_operations SET created_at = now() - interval '2 minutes' WHERE user_id = '{order['user_id']}'",
        ]
    )
    active, _ = create_order(buyer_token, book_id, 1, expected=409)
    if active["error_code"] != "ORDER_HAS_ACTIVE":
        raise SystemExit(active)
    request("POST", "/orders", {"items": [{"product_id": book_id, "quantity": 1}]}, token=seller_token, expected=403)
    owned, _ = request("GET", f"/orders/{order_id}", token=other_token, expected=403)
    if owned["error_code"] != "ORDER_OWNERSHIP_VIOLATION":
        raise SystemExit(owned)
    request("GET", f"/orders/{order_id}", token=seller_token, expected=403)
    request("GET", f"/orders/{order_id}", token=admin_token, expected=200)

    updated, _ = request(
        "PUT",
        f"/orders/{order_id}",
        {"items": [{"product_id": book_id, "quantity": 1}]},
        token=buyer_token,
    )
    if updated["total_amount"] != 100 or stock_of(buyer_token, book_id) != before - 1:
        raise SystemExit(f"обновление сломало остаток или сумму: {updated}")
    limited, _ = request(
        "PUT",
        f"/orders/{order_id}",
        {"items": [{"product_id": book_id, "quantity": 1}]},
        token=buyer_token,
        expected=429,
    )
    if limited["error_code"] != "ORDER_LIMIT_EXCEEDED":
        raise SystemExit(limited)

    request(
        "PUT",
        f"/products/{book_id}",
        product_body(price=150, stock=stock_of(seller_token, book_id)),
        token=seller_token,
    )
    current, _ = request("GET", f"/orders/{order_id}", token=buyer_token)
    if current["items"][0]["price_at_order"] != 100:
        raise SystemExit("снимок цены изменился вместе с карточкой")

    pending, _ = request("POST", f"/orders/{order_id}/advance", token=buyer_token)
    if pending["status"] != "PAYMENT_PENDING":
        raise SystemExit(pending)
    blocked, _ = request(
        "PUT",
        f"/orders/{order_id}",
        {"items": [{"product_id": book_id, "quantity": 1}]},
        token=buyer_token,
        expected=409,
    )
    if blocked["error_code"] != "INVALID_STATE_TRANSITION":
        raise SystemExit(blocked)
    canceled, _ = request("POST", f"/orders/{order_id}/cancel", token=buyer_token)
    if canceled["status"] != "CANCELED" or stock_of(buyer_token, book_id) != before:
        raise SystemExit(f"отмена не вернула остаток: {canceled}")
    _, rate_buyer, _, _ = register("USER")
    rate_order, _ = create_order(rate_buyer, book_id, 1)
    request("POST", f"/orders/{rate_order['id']}/cancel", token=rate_buyer)
    again, _ = create_order(rate_buyer, book_id, 1, expected=429)
    if again["error_code"] != "ORDER_LIMIT_EXCEEDED":
        raise SystemExit(again)

    # Цена товара теперь 150. Дальше считаем от неё.
    promo, _ = request(
        "POST",
        "/promo-codes",
        {
            "code": f"SAVE{uuid.uuid4().hex[:6].upper()}",
            "discount_type": "PERCENTAGE",
            "discount_value": 10,
            "min_order_amount": 50,
            "max_uses": 5,
            "valid_from": "2020-01-01T00:00:00Z",
            "valid_until": "2035-01-01T00:00:00Z",
        },
        token=seller_token,
        expected=201,
    )
    request(
        "POST",
        "/promo-codes",
        {
            "code": "USERONLY",
            "discount_type": "FIXED_AMOUNT",
            "discount_value": 10,
            "min_order_amount": 0,
            "max_uses": 1,
            "valid_from": "2020-01-01T00:00:00Z",
            "valid_until": "2035-01-01T00:00:00Z",
        },
        token=buyer_token,
        expected=403,
    )
    _, promo_buyer, _, _ = register("USER")
    priced, _ = create_order(promo_buyer, book_id, 2, promo=promo["code"])
    # 2 * 150 = 300, скидка 10% = 30.
    if priced["discount_amount"] != 30 or priced["total_amount"] != 270 or priced["promo_code"] != promo["code"]:
        raise SystemExit(f"промокод посчитан неверно: {priced}")
    state = priced
    for expected_status in ("PAYMENT_PENDING", "PAID"):
        state, _ = request("POST", f"/orders/{state['id']}/advance", token=promo_buyer)
        if state["status"] != expected_status:
            raise SystemExit(state)
    invalid, _ = request("POST", f"/orders/{state['id']}/cancel", token=promo_buyer, expected=409)
    if invalid["error_code"] != "INVALID_STATE_TRANSITION":
        raise SystemExit(invalid)
    for expected_status in ("SHIPPED", "COMPLETED"):
        state, _ = request("POST", f"/orders/{state['id']}/advance", token=promo_buyer)
        if state["status"] != expected_status:
            raise SystemExit(state)
    finished, _ = request("POST", f"/orders/{state['id']}/advance", token=promo_buyer, expected=409)
    if finished["error_code"] != "INVALID_STATE_TRANSITION":
        raise SystemExit(finished)

    _, min_buyer, _, _ = register("USER")
    min_code = f"MIN{uuid.uuid4().hex[:6].upper()}"
    request(
        "POST",
        "/promo-codes",
        {
            "code": min_code,
            "discount_type": "PERCENTAGE",
            "discount_value": 10,
            "min_order_amount": 1000,
            "max_uses": 5,
            "valid_from": "2020-01-01T00:00:00Z",
            "valid_until": "2035-01-01T00:00:00Z",
        },
        token=seller_token,
        expected=201,
    )
    too_small, _ = create_order(min_buyer, book_id, 1, promo=min_code, expected=422)
    if too_small["error_code"] != "PROMO_CODE_MIN_AMOUNT":
        raise SystemExit(too_small)
    missing_promo, _ = create_order(min_buyer, book_id, 1, promo="NO_SUCH_CODE", expected=422)
    if missing_promo["error_code"] != "PROMO_CODE_INVALID":
        raise SystemExit(missing_promo)

    full_code = f"FULL{uuid.uuid4().hex[:6].upper()}"
    request(
        "POST",
        "/promo-codes",
        {
            "code": full_code,
            "discount_type": "PERCENTAGE",
            "discount_value": 80,
            "min_order_amount": 0,
            "max_uses": 5,
            "valid_from": "2020-01-01T00:00:00Z",
            "valid_until": "2035-01-01T00:00:00Z",
        },
        token=seller_token,
        expected=201,
    )
    full_price, _ = create_order(min_buyer, book_id, 1, promo=full_code)
    if full_price["discount_amount"] != 0 or full_price["total_amount"] != 150:
        raise SystemExit(f"скидка выше 70% должна оставить полную цену: {full_price}")

    _, fixed_buyer, _, _ = register("USER")
    fixed_code = f"FIX{uuid.uuid4().hex[:6].upper()}"
    request(
        "POST",
        "/promo-codes",
        {
            "code": fixed_code,
            "discount_type": "FIXED_AMOUNT",
            "discount_value": 30,
            "min_order_amount": 0,
            "max_uses": 5,
            "valid_from": "2020-01-01T00:00:00Z",
            "valid_until": "2035-01-01T00:00:00Z",
        },
        token=seller_token,
        expected=201,
    )
    fixed, _ = create_order(fixed_buyer, book_id, 1, promo=fixed_code)
    if fixed["discount_amount"] != 30 or fixed["total_amount"] != 120:
        raise SystemExit(f"фиксированная скидка: {fixed}")

    rare, _ = request(
        "POST",
        "/products",
        product_body(name="Rare", price=10, stock=1, category="rare"),
        token=seller_token,
        expected=201,
    )
    _, short_buyer, _, _ = register("USER")
    short, _ = create_order(short_buyer, rare["id"], 5, expected=409)
    if short["error_code"] != "INSUFFICIENT_STOCK" or short["details"]["items"][0]["requested"] != 5:
        raise SystemExit(short)
    if stock_of(short_buyer, rare["id"]) != 1:
        raise SystemExit("неуспешный заказ изменил остаток")

    inactive, _ = request(
        "POST",
        "/products",
        product_body(name="Draft", status="INACTIVE", category="drafts"),
        token=seller_token,
        expected=201,
    )
    inactive_order, _ = create_order(short_buyer, inactive["id"], 1, expected=409)
    if inactive_order["error_code"] != "PRODUCT_INACTIVE":
        raise SystemExit(inactive_order)
    missing, _ = create_order(short_buyer, str(uuid.uuid4()), 1, expected=404)
    if missing["error_code"] != "PRODUCT_NOT_FOUND":
        raise SystemExit(missing)

    doomed, _ = request(
        "POST",
        "/products",
        product_body(name="Doomed", category="doomed"),
        token=seller_token,
        expected=201,
    )
    request("DELETE", f"/products/{doomed['id']}", token=second_seller, expected=403)
    archived, _ = request("DELETE", f"/products/{doomed['id']}", token=seller_token)
    if archived["status"] != "ARCHIVED":
        raise SystemExit(archived)
    archived_order, _ = create_order(short_buyer, doomed["id"], 1, expected=409)
    if archived_order["error_code"] != "PRODUCT_INACTIVE":
        raise SystemExit(archived_order)

    drop_code = f"DROP{uuid.uuid4().hex[:4].upper()}"
    request(
        "POST",
        "/promo-codes",
        {
            "code": drop_code,
            "discount_type": "PERCENTAGE",
            "discount_value": 10,
            "min_order_amount": 200,
            "max_uses": 3,
            "valid_from": "2020-01-01T00:00:00Z",
            "valid_until": "2035-01-01T00:00:00Z",
        },
        token=admin_token,
        expected=201,
    )
    _, drop_buyer, _, _ = register("USER")
    # 2 * 150 = 300 >= 200, скидка 30, итог 270.
    dropped, _ = create_order(drop_buyer, book_id, 2, promo=drop_code)
    if dropped["discount_amount"] != 30:
        raise SystemExit(dropped)
    cleared, _ = request(
        "PUT",
        f"/orders/{dropped['id']}",
        {"items": [{"product_id": book_id, "quantity": 1}]},
        token=drop_buyer,
    )
    if cleared["promo_code"] is not None or cleared["discount_amount"] != 0 or cleared["total_amount"] != 150:
        raise SystemExit(f"промокод не снялся: {cleared}")

    refreshed, _ = request("POST", "/auth/refresh", {"refresh_token": buyer_refresh})
    request("POST", "/auth/refresh", {"refresh_token": buyer_refresh}, expected=401)
    request("POST", "/auth/refresh", {"refresh_token": refreshed["refresh_token"]}, expected=200)

    log_container = os.environ.get("LOG_CONTAINER")
    if log_container:
        logs = subprocess.check_output(["docker", "logs", log_container], text=True)
    else:
        logs = subprocess.check_output(["docker", "compose", "logs", "core"], text=True)
    if PASSWORD in logs:
        raise SystemExit("пароль попал в логи")
    found_user = False
    for line in logs.splitlines():
        start = line.find("{")
        if start < 0:
            continue
        try:
            record = json.loads(line[start:])
        except json.JSONDecodeError:
            continue
        for key in ("request_id", "method", "endpoint", "status_code", "duration_ms", "user_id", "timestamp"):
            if key not in record:
                raise SystemExit(f"в логе нет {key}: {record}")
        if record.get("endpoint") == "/orders" and record.get("method") == "POST" and record.get("user_id"):
            found_user = True
    if not found_user:
        raise SystemExit("нет JSON-лога авторизованного POST /orders")

    uses = subprocess.check_output(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            "marketplace",
            "-d",
            "marketplace",
            "-tAc",
            f"SELECT current_uses FROM promo_codes WHERE code = '{drop_code}'",
        ],
        text=True,
    ).strip()
    if uses != "0":
        raise SystemExit(f"использование промокода не вернулось: {uses!r}")

    rows = subprocess.check_output(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            "marketplace",
            "-d",
            "marketplace",
            "-tAc",
            f"SELECT status, total_amount FROM orders WHERE id = '{priced['id']}'",
        ],
        text=True,
    ).strip()
    status, total = rows.split("|")
    if status != "COMPLETED" or float(total) != 270:
        raise SystemExit(f"в базе неожиданная строка заказа: {rows!r}")
    print("e2e ok")


if __name__ == "__main__":
    try:
        main()
    except ApiError as err:
        raise SystemExit(err) from err
