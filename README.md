# Weather Tracker

Weather Tracker is a small Go application that exposes current weather data for a latitude and longitude pair. It is built as two services inside one Go module:

- A client/API gateway that exposes an HTTP endpoint `/weather`.
- A server/backend service that exposes a gRPC API, talks to PostgreSQL (for caching), and fetches data from Open-Meteo when the cache does not have fresh data.

The application returns:

- temperature
- humidity
- elevation

## How It Works

The public entry point is the HTTP client service. A caller sends:

```http
GET /weather?lat=25.2048&lon=55.2708
```

The request flow is:

1. The HTTP client receives `lat` and `lon` through the OpenAPI-generated handler in `cmd/client/api`.
2. The client creates a gRPC `GetWeatherRequest` and sends it to the backend server.
3. The backend converts the latitude and longitude into a geohash with precision `5` (each geohash value represents an area of ~24 square km).
4. The backend checks the database (PostgreSQL) for a recent cached weather row for that geohash.
5. If the row exists and was created within the last 5 minutes, the server returns the cached data.
6. If no fresh row exists, or if the cache lookup fails, the server calls the Open-Meteo API.
7. The server stores the returned weather data in the database using an upsert.
8. The server returns the weather response over gRPC.
9. The client converts the gRPC response to JSON and sends it back to the HTTP caller.

The client uses `GRPC_ADDR` to communicate with the gRPC server. The server uses `DATABASE_URL` to connect to a PostgreSQL database.

## Project Structure

```text
.
├── cmd/
│   ├── client/
│   │   ├── api/
│   │   │   └── api.gen.go
│   │   ├── main.go
│   │   └── main_test.go
│   └── server/
│       ├── main.go
│       ├── main_test.go
│       ├── openmeteo.go
│       ├── openmeteo_test.go
│       └── testutils_test.go
├── db/
│   ├── queries.sql
│   └── schema.sql
├── docs/
│   └── openapi.yaml
├── internal/
│   ├── assert/
│   │   └── assert.go
│   └── weatherdb/
│       ├── db.go
│       ├── models.go
│       └── queries.sql.go
├── pb/
│   └── weather/v1/
│       ├── weather.proto
│       ├── weather.pb.go
│       └── weather_grpc.pb.go
├── Dockerfile
├── README.md
├── docker-compose.yml
├── go.mod
├── go.sum
├── makefile
└── sqlc.yaml
```

### Why This Structure

The project keeps the client and server in separate top-level folders because they are separate runtime processes with different responsibilities. The client owns the HTTP endpoints. The server owns the gRPC service, cache logic, database access, and external API integration.

They still live inside the same Go module because this is one application, not two independently versioned products. Keeping them together makes sense here because:

- both services share the same protobuf under `pb/weather/v1`;
- Docker Compose builds and runs them together;
- generated code stays in one repository;
- local development is simpler;
- changes to the HTTP gateway, gRPC contract, server logic, and database queries can be reviewed together.

If the services grew into independently deployed products owned by different teams, splitting them into separate repositories could make sense. For this project, one module gives cleaner coordination with less overhead.

## Database Schema

The database schema lives in `db/schema.sql`.

```sql
CREATE TABLE IF NOT EXISTS weather (
  id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  geohash VARCHAR(12) NOT NULL UNIQUE,
  temperature DOUBLE PRECISION NOT NULL,
  humidity DOUBLE PRECISION NOT NULL,
  elevation DOUBLE PRECISION NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Columns

`id` is the primary key for each database row.

`geohash` is the cache key derived from latitude and longitude. It is unique so one geohash maps to one cached weather row.

`temperature` stores the current temperature returned by Open-Meteo.

`humidity` stores the current humidity returned by Open-Meteo.

`elevation` stores the elevation returned by Open-Meteo.

`created_at` records when the cached row was created or refreshed. The server uses this to decide whether the cached value is still fresh.

## Database Queries

The SQL queries live in `db/queries.sql` and are used by `sqlc` to generate the typed Go database package in `internal/weatherdb`.

### GetWeather

```sql
-- name: GetWeather :one
SELECT created_at, temperature, humidity, elevation
FROM weather
WHERE geohash = $1 AND created_at BETWEEN NOW() - INTERVAL '5 minutes' AND NOW();
```

This query attempts to load a fresh cached weather row for a geohash. It only returns data if `created_at` is within the last 5 minutes.

If a row is older than 5 minutes, the query behaves like there is no usable cache row. The server treats that as a cache miss, calls Open-Meteo, and stores fresh data.

### SetWeather

```sql
-- name: SetWeather :exec
INSERT INTO
weather (geohash, temperature, humidity, elevation)
VALUES ($1, $2, $3, $4)
ON CONFLICT (geohash)
DO UPDATE SET
temperature = EXCLUDED.temperature,
humidity = EXCLUDED.humidity,
elevation = EXCLUDED.elevation,
created_at = now();
```

This query stores weather data in the cache.

If the geohash does not exist yet, it inserts a new row. If the geohash already exists, PostgreSQL updates the existing row instead of failing with a duplicate key error. The `EXCLUDED` values are the values from the insert attempt, and `created_at = now()` refreshes the cache timestamp.

This matters because the cache lookup ignores rows older than 5 minutes. Without the upsert, refreshing an old geohash would fail because the unique geohash row would already exist.

## Client

The client service is the HTTP-facing API gateway. It listens on port `8080`.

Main endpoint:

```http
GET /weather?lat={latitude}&lon={longitude}
```

Successful response:

```json
{
  "temperature": 31.4,
  "humidity": 52,
  "elevation": 12
}
```

The HTTP route and request parameter binding are generated from `docs/openapi.yaml` into `cmd/client/api`. The client does not talk to PostgreSQL or Open-Meteo directly. Its job is to validate the HTTP request shape, call the gRPC backend, and return JSON.

If the gRPC backend returns an error, the client returns HTTP `500`.

## Server

The server service is the backend weather service. It listens for gRPC requests on port `50051`.

Its gRPC contract is defined in `pb/weather/v1/weather.proto`:

```proto
service WeatherService {
    rpc GetWeather(GetWeatherRequest) returns (GetWeatherResponse);
}
```

The server is responsible for:

- converting latitude and longitude to a geohash;
- checking PostgreSQL for a fresh cached result;
- calling Open-Meteo when the cache misses or the cache lookup fails;
- saving fresh weather data through the generated `sqlc` query package;
- returning temperature, humidity, and elevation to the HTTP client over gRPC.

The server uses `database/sql` with the pgx driver and generated `sqlc` methods from `internal/weatherdb`.

## Docker Setup

The Dockerfile is shared by both runtime services. Compose passes `APP=cmd/server` or `APP=cmd/client` as a build argument so the same Dockerfile can build either binary.

Docker Compose defines four services:

- `db`: PostgreSQL with `db/schema.sql` mounted as the first-run initialization script.
- `server`: the gRPC backend, connected to PostgreSQL through `DATABASE_URL`.
- `client`: the HTTP API gateway, connected to the backend through `GRPC_ADDR`.
- `test`: a profiled one-shot Go test runner built from the Dockerfile `test` stage.

The `test` service uses a Compose profile so normal app startup does not run tests every time. Use it only when you explicitly want containerized tests.

## Testing

The test suite covers the HTTP client gateway, gRPC server behavior, database cache interactions, and Open-Meteo response handling. It uses a fake gRPC client for client tests, a small fake SQL driver for server cache tests, a fake HTTP transport for Open-Meteo tests, and a shared `internal/assert` helper for concise assertions. Because of this, the tests can run without starting PostgreSQL, Docker Compose, or the real Open-Meteo API.

Run all tests:

```bash
go test ./...
```

Run tests with package-level coverage:

```bash
go test -cover ./...
```

Generate a coverage profile and print function-level coverage in the terminal:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

To inspect coverage in a browser:

```bash
go tool cover -html=coverage.out
```

## Development Commands

Generate database query code:

```bash
make generate-db
```

Run tests:

```bash
make test
```

Run tests through Docker Compose:

```bash
docker-compose --profile test run --rm test
```

Generate database code and run tests:

```bash
make verify
```

Build and run the application with Docker Compose:

```bash
make docker-up
```

Stop the application:

```bash
make docker-down
```

The direct Docker Compose equivalents are:

```bash
docker-compose up --build --force-recreate
docker-compose down
```
