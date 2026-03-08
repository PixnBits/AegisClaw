# Use a small static base — we want very little attack surface
FROM alpine:3.19

# Install only ca-certificates (for future https if needed) + tzdata if you want logging with time
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the compiled binary (we build outside and copy — multi-stage later)
COPY messagehub.go /app/messagehub.go

# For MVP we build inside the image (small image, Go is available)
RUN apk add --no-cache go \
    && go mod init seedclaw/message-hub \
    && go build -o /message-hub ./messagehub.go \
    && rm -rf /go/pkg /go/src \
    && apk del go

# Drop to non-root
USER 10001:10001

# The socket will be mounted from host here
VOLUME /run/sockets/control/seedclaw.sock

CMD ["/message-hub"]
