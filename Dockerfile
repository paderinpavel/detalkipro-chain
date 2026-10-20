FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN go build -o detalki-node main.go

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/detalki-node .
RUN mkdir -p /data
ENV DATA_DIR=/data
ENV PORT=8080
EXPOSE 8080
CMD ["./detalki-node"]
