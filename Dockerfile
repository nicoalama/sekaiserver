FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /sekai-server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 1001 sekai
COPY --from=builder /sekai-server /usr/local/bin/sekai-server
USER sekai
EXPOSE 11434
ENTRYPOINT ["sekai-server"]
