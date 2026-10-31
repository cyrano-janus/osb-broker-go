# Build stage
FROM golang:1.26-alpine AS builder

# Das offizielle golang-Image setzt GOTOOLCHAIN=local und laedt deshalb NIE
# eine Toolchain nach: verlangt go.mod mehr, als das Image mitbringt, bricht
# `go mod download` ab mit "go.mod requires go >= X (running go Y;
# GOTOOLCHAIN=local)". Genau das ist beim Heben auf 1.26.10 passiert, solange
# `golang:1.26-alpine` lokal noch auf 1.26.9 stand.
#
# Mit `auto` deckt die go-Zeile in go.mod wieder alle drei Bauwege ab - CI,
# lokaler Bau, Container -, so wie es der Kommentar dort beschreibt. Kostet
# nichts, wenn das Image ohnehin passt: dann laedt `auto` nichts nach.
ENV GOTOOLCHAIN=auto

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
