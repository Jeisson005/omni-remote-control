FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/omni-server cmd/server/main.go

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /bin/omni-server /app/omni-server

EXPOSE 8090

ENTRYPOINT ["/app/omni-server"]
