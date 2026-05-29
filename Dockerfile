FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /goexec ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /goexec /goexec
EXPOSE 8080
ENTRYPOINT ["/goexec"]
