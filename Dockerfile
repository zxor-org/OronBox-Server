FROM golang:alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oronbox-server ./cmd/server

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S oronbox \
    && adduser -S -G oronbox -h /var/lib/oronbox oronbox \
    && mkdir -p /var/lib/oronbox \
    && chown -R oronbox:oronbox /var/lib/oronbox
WORKDIR /var/lib/oronbox
COPY --from=builder /out/oronbox-server /usr/local/bin/oronbox-server
USER oronbox
EXPOSE 6767
ENTRYPOINT ["/usr/local/bin/oronbox-server"]
