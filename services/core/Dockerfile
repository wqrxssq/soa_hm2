FROM golang:1.27-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config api/openapi/codegen.yaml -o internal/api/openapi.gen.go api/openapi/openapi.yaml
RUN gofmt -w internal/api/openapi.gen.go && test -z "$$(gofmt -l .)"
RUN CGO_ENABLED=0 go test ./...
RUN CGO_ENABLED=0 go build -o /out/core ./cmd/core

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/core /app/core
USER nobody
EXPOSE 8080
ENTRYPOINT ["/app/core"]
