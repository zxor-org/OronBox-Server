FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oronbox-server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S oronbox && adduser -S -G oronbox oronbox
COPY --from=builder /out/oronbox-server /usr/local/bin/oronbox-server
RUN mkdir -p /var/lib/oronbox/blobs && chown -R oronbox:oronbox /var/lib/oronbox
USER oronbox
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/oronbox-server"]
