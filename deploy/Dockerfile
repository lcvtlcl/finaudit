# --- build ---
FROM golang:1.25-alpine AS build
WORKDIR /src
ARG GIT_SHA=dev
ARG BUILD_TIME=unknown
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build \
  -ldflags "-X github.com/wwpp/finaudit/internal/api.BuildCommit=${GIT_SHA} -X github.com/wwpp/finaudit/internal/api.BuildTime=${BUILD_TIME}" \
  -o /out/server ./cmd/server
# --- run ---
FROM alpine:3.20
RUN adduser -D -u 10001 app
RUN mkdir -p /data/uploads && chown -R app:app /data
WORKDIR /app
COPY --from=build /out/server /app/server
COPY web /app/web
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]
