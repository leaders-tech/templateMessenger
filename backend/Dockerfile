FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/messenger-backend ./cmd/messenger

FROM alpine:3.20

RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/messenger-backend /app/messenger-backend
USER app

EXPOSE 8081
ENTRYPOINT ["/app/messenger-backend"]

