FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /app ./cmd/taillight

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /app
# Config via environment variables (DATABASE_URL, LISTEN_ADDR, etc.)
ENTRYPOINT ["/app"]
CMD ["serve"]
