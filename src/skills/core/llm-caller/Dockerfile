# Use a small static base — we want very little attack surface
FROM alpine:3.19

# Install only ca-certificates (for https) + tzdata if you want logging with time
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the compiled binary (we build outside and copy — multi-stage later)
COPY llmcaller.go /app/llmcaller.go

# For MVP we build inside the image (small image, Go is available)
RUN apk add --no-cache go \
    && go mod init seedclaw/llm-caller \
    && go build -o /llm-caller ./llmcaller.go \
    && rm -rf /go/pkg /go/src \
    && apk del go

# Drop to non-root
USER 10001:10001