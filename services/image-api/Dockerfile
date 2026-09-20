# Stage 1: Builder
FROM golang:1.27-alpine AS builder

RUN apk add --no-cache gcc musl-dev libc-dev ca-certificates git tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /app/server cmd/server/main.go

# Stage 2: Runtime
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 -H -s /sbin/nologin appuser

WORKDIR /

COPY --from=builder /app/server /app/server
COPY --from=builder /src/migrations /migrations

ENV APP_ENV=production
ENV PORT=8080
ENV TZ=UTC

EXPOSE 8080

USER appuser

ENTRYPOINT ["/app/server"]
