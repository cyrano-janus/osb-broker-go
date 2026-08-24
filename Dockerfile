# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /broker .

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /broker /broker

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/broker"]
