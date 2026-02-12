FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /app ./cmd/taillight

FROM alpine:3.21
RUN adduser -D -u 65532 nonroot
USER nonroot
COPY --from=build /app /app
COPY --from=build /src/migrations /migrations
# Run pending migrations then start the server.
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["/app migrate up --path /migrations && /app serve"]
