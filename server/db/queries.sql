-- name: GetWeather :one
SELECT created_at, temperature, humidity, elevation
FROM weather
WHERE geohash = $1 AND created_at BETWEEN NOW() - INTERVAL '5 minutes' AND NOW();

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