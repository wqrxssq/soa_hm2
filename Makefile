.PHONY: generate fmt test up down e2e

generate:
	cd services/core && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config api/openapi/codegen.yaml -o internal/api/openapi.gen.go api/openapi/openapi.yaml
	cd services/core && gofmt -w internal/api/openapi.gen.go

fmt:
	cd services/core && gofmt -w .

test: generate
	cd services/core && test -z "$$(gofmt -l .)"
	cd services/core && go test ./...

up:
	docker compose up --build

down:
	docker compose down -v

e2e:
	python3 scripts/e2e.py
