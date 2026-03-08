FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /stellar-drive ./cmd/stellar-drive

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /stellar-drive /usr/local/bin/stellar-drive
COPY benchmarks/schemas/ /app/benchmarks/schemas/
COPY benchmarks/stellar.yaml /app/benchmarks/stellar.yaml

WORKDIR /app

ENV STELLAR_SERVER_PORT=8200
ENV STELLAR_MONGO_URI=mongodb://mongo:27017
ENV STELLAR_MONGO_DATABASE=stellar_drive_bench

EXPOSE 8200

HEALTHCHECK --interval=5s --timeout=3s --retries=10 \
  CMD wget -qO- http://localhost:${STELLAR_SERVER_PORT}/health || exit 1

CMD ["stellar-drive", "--config=/app/benchmarks/stellar.yaml", "run"]
