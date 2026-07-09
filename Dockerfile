# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS source

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

FROM source AS test

CMD ["go", "test", "./..."]

FROM source AS build

ARG APP=cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/weather-app ./${APP}

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& adduser -D -H -u 10001 appuser

WORKDIR /app

COPY --from=build /out/weather-app /app/weather-app

USER appuser

EXPOSE 8080 50051

ENTRYPOINT ["/app/weather-app"]
