FROM golang:1.27 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /kubepilot ./cmd/kubepilot
FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /kubepilot /usr/local/bin/kubepilot
RUN mkdir -p /app/data && chown 65532:65532 /app/data
WORKDIR /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["kubepilot"]
