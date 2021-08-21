# Build the go application into a binary
FROM golang:alpine as builder
WORKDIR /app
ADD . ./
RUN CGO_ENABLED=0 GOOS=linux go build -mod vendor -a -installsuffix cgo -o bin/discord-channel-proxy-bot .
RUN apk --update add ca-certificates

FROM scratch
ENV APP_HOME=/app
ENV DISCORD_BOT_TOKEN=""
WORKDIR ${APP_HOME}
COPY --from=builder /app/bin/discord-channel-proxy-bot ./bin/discord-channel-proxy-bot
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/app/bin/discord-channel-proxy-bot"]