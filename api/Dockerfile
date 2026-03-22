FROM golang:1.26-alpine AS build
WORKDIR /src
# Copy both modules so the replace directive resolves.
COPY api/go.mod api/go.sum ./api/
COPY pkg/logshipper/go.mod ./pkg/logshipper/
RUN cd api && go mod download
COPY pkg/logshipper/ ./pkg/logshipper/
COPY api/ ./api/
ARG VERSION=dev
RUN cd api && CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /app ./cmd/taillight

FROM alpine:3.23
RUN adduser -D -u 65532 nonroot
USER nonroot
COPY --from=build /app /app
COPY --from=build /src/api/migrations /migrations
# Run pending migrations (with retry for slow postgres startup) then start the server.
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["for i in 1 2 3 4 5; do /app migrate up --path /migrations && break; echo \"Migration attempt $i failed, retrying in 3s...\"; sleep 3; done && exec /app serve"]
