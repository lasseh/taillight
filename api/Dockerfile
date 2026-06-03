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
COPY --from=build /src/api/reference /reference
# Run pending migrations (with retry for slow postgres startup) then start the
# server. serve is exec'd only on a successful migrate; if all attempts fail the
# container exits non-zero (fail-fast) rather than booting on a half-migrated
# schema. A for-loop's exit status is its last command's (sleep, always 0), so
# the success path must be inside the loop, not gated on the loop's status.
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["for i in 1 2 3 4 5; do /app migrate up --path /migrations && exec /app serve; echo \"Migration attempt $i failed, retrying in 3s...\"; sleep 3; done; echo 'migrations failed after 5 attempts, aborting'; exit 1"]
