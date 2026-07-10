# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS source

WORKDIR /src

COPY go.work go.work.sum ./
COPY client/go.mod client/go.sum ./client/
COPY pb/go.mod pb/go.sum ./pb/
COPY server/go.mod server/go.sum ./server/
RUN cd pb && go mod download \
	&& cd ../server && go mod download \
	&& cd ../client && go mod download

COPY . .

FROM source AS test

CMD ["go", "test", "./client/...", "./server/..."]

FROM source AS build

ARG APP=server/cmd
RUN case "${APP}" in \
	cmd/server) APP=server/cmd ;; \
	cmd/client) APP=client/cmd ;; \
	esac; \
	CGO_ENABLED=0 GOOS=linux go build -o /out/weather-app "./${APP}"

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& adduser -D -H -u 10001 appuser

WORKDIR /app

COPY --from=build /out/weather-app /app/weather-app

USER appuser

EXPOSE 8080 50051

ENTRYPOINT ["/app/weather-app"]
