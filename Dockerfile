FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ceksepet-api .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && addgroup -S ceksepet \
    && adduser -S -G ceksepet ceksepet

WORKDIR /app
COPY --from=build /out/ceksepet-api /usr/local/bin/ceksepet-api
USER ceksepet
EXPOSE 8080
ENTRYPOINT ["ceksepet-api"]
