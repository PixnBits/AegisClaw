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

# Prepare socket directory and drop to non-root
RUN mkdir -p /run/sockets/control \
    && chown 10001:10001 /run/sockets/control \
    && chmod 0750 /run/sockets/control

USER 10001:10001

# The socket will be mounted from host in this directory
VOLUME /run/sockets/control/

CMD ["/message-hub"]
