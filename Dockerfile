FROM alpine:latest

RUN apk --update add ca-certificates

WORKDIR /app
ADD cloudroutesync cloudroutesync

ENTRYPOINT ["/app/cloudroutesync"]