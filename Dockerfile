FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /so-budget ./cmd/so-budget

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
RUN mkdir -p /data && chown app:app /data
USER app
WORKDIR /app
COPY --from=build /so-budget /usr/local/bin/so-budget
ENV SB_DB_PATH=/data/so-budget.db SB_LISTEN=:8080 SB_BACKUP_DIR=/data/backups
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["so-budget"]
CMD ["serve"]
