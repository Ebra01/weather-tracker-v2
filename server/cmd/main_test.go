package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"
	"time"

	weatherv1 "weather-tracker/pb/weather/v1"
	"weather-tracker/server/internal/assert"

	"github.com/mmcloughlin/geohash"
)

func TestEnvOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		fallback string
		want     string
	}{
		{
			name:     "Existing value",
			key:      "DATABASE_URL",
			value:    "postgres://test-db/weatherapp",
			fallback: "postgres://localhost/weatherapp",
			want:     "postgres://test-db/weatherapp",
		},
		{
			name:     "Fallback value",
			key:      "DATABASE_URL",
			fallback: "postgres://localhost/weatherapp",
			want:     "postgres://localhost/weatherapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)

			got := envOrDefault(tt.key, tt.fallback)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestServerGetTemperature(t *testing.T) {
	const (
		lat  = 25.2048
		long = 55.2708
	)

	hash := geohash.EncodeWithPrecision(lat, long, geoHashPrecision)

	tests := []struct {
		name      string
		queryRow  []driver.Value
		queryErr  error
		wantFound bool
		wantErr   bool
		want      Result
	}{
		{
			name: "Cache hit",
			queryRow: []driver.Value{
				time.Now(),
				31.4,
				52.0,
				12.5,
			},
			wantFound: true,
			want: Result{
				Geohash:     hash,
				Temperature: 31.4,
				Humidity:    52.0,
				Elevation:   12.5,
			},
		},
		{
			name:      "Cache miss",
			queryErr:  sql.ErrNoRows,
			wantFound: false,
			want:      Result{Geohash: hash},
		},
		{
			name:      "Database error",
			queryErr:  errors.New("database unavailable"),
			wantFound: false,
			wantErr:   true,
			want:      Result{Geohash: hash},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &testDBState{
				queryRow: tt.queryRow,
				queryErr: tt.queryErr,
			}
			srv := &server{queries: newTestQueries(t, state)}

			got, found, err := srv.GetDataFromCache(context.Background(), lat, long)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
			} else {
				assert.Nil(t, err)
			}

			assert.Equal(t, found, tt.wantFound)
			assert.Equal(t, got.Geohash, tt.want.Geohash)
			assert.Equal(t, got.Temperature, tt.want.Temperature)
			assert.Equal(t, got.Humidity, tt.want.Humidity)
			assert.Equal(t, got.Elevation, tt.want.Elevation)
			assert.Equal(t, state.queryArgs[0].Value.(string), hash)
		})
	}
}

func TestServerSaveTemperature(t *testing.T) {
	tests := []struct {
		name    string
		execErr error
		wantErr bool
	}{
		{
			name: "Save succeeds",
		},
		{
			name:    "Save fails",
			execErr: errors.New("insert failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &testDBState{execErr: tt.execErr}
			srv := &server{queries: newTestQueries(t, state)}

			err := srv.SaveTemperature(context.Background(), Result{
				Geohash:     "thrn0",
				Temperature: 31.4,
				Humidity:    52.0,
				Elevation:   12.5,
			})

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
			} else {
				assert.Nil(t, err)
			}

			assert.True(t, state.execCalled)
			assert.Equal(t, state.execArgs[0].Value.(string), "thrn0")
			assert.Equal(t, state.execArgs[1].Value.(float64), 31.4)
			assert.Equal(t, state.execArgs[2].Value.(float64), 52.0)
			assert.Equal(t, state.execArgs[3].Value.(float64), 12.5)
		})
	}
}

func TestServerGetWeatherCacheHit(t *testing.T) {
	state := &testDBState{
		queryRow: []driver.Value{
			time.Now(),
			31.4,
			52.0,
			12.5,
		},
	}
	srv := &server{queries: newTestQueries(t, state)}

	setTestHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("OpenMeteo should not be called for a cache hit")
		return nil, nil
	})

	resp, err := srv.GetWeather(context.Background(), &weatherv1.GetWeatherRequest{
		Latitude:  25.2048,
		Longitude: 55.2708,
	})
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, resp.Temperature, 31.4)
	assert.Equal(t, resp.Humidity, 52.0)
	assert.Equal(t, resp.Elevation, 12.5)
	assert.Equal(t, state.execCalled, false)
}

func TestServerGetWeatherCacheMissFetchesAndSaves(t *testing.T) {
	const (
		lat  = 25.2048
		long = 55.2708
	)

	state := &testDBState{queryErr: sql.ErrNoRows}
	srv := &server{queries: newTestQueries(t, state)}

	setTestHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, r.URL.Host, "api.open-meteo.com")
		assert.Equal(t, r.URL.Query().Get("latitude"), "25.2048")
		assert.Equal(t, r.URL.Query().Get("longitude"), "55.2708")

		return testHTTPResponse(http.StatusOK, `{
			"latitude": 25.2048,
			"longitude": 55.2708,
			"elevation": 12.5,
			"current": {
				"time": "2026-07-07T10:00",
				"temperature_2m": 31.4,
				"relative_humidity_2m": 52
			}
		}`), nil
	})

	resp, err := srv.GetWeather(context.Background(), &weatherv1.GetWeatherRequest{
		Latitude:  lat,
		Longitude: long,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantGeohash := geohash.EncodeWithPrecision(lat, long, geoHashPrecision)

	assert.Equal(t, resp.Temperature, 31.4)
	assert.Equal(t, resp.Humidity, 52.0)
	assert.Equal(t, resp.Elevation, 12.5)
	assert.True(t, state.execCalled)
	assert.Equal(t, state.execArgs[0].Value.(string), wantGeohash)
	assert.Equal(t, state.execArgs[1].Value.(float64), 31.4)
	assert.Equal(t, state.execArgs[2].Value.(float64), 52.0)
	assert.Equal(t, state.execArgs[3].Value.(float64), 12.5)
}

func TestServerGetWeatherDatabaseErrorFetchesAndSaves(t *testing.T) {
	const (
		lat  = 25.2048
		long = 55.2708
	)

	state := &testDBState{queryErr: errors.New("database unavailable")}
	srv := &server{queries: newTestQueries(t, state)}

	setTestHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, r.URL.Host, "api.open-meteo.com")
		assert.Equal(t, r.URL.Query().Get("latitude"), "25.2048")
		assert.Equal(t, r.URL.Query().Get("longitude"), "55.2708")

		return testHTTPResponse(http.StatusOK, `{
			"latitude": 25.2048,
			"longitude": 55.2708,
			"elevation": 12.5,
			"current": {
				"time": "2026-07-07T10:00",
				"temperature_2m": 31.4,
				"relative_humidity_2m": 52
			}
		}`), nil
	})

	resp, err := srv.GetWeather(context.Background(), &weatherv1.GetWeatherRequest{
		Latitude:  lat,
		Longitude: long,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantGeohash := geohash.EncodeWithPrecision(lat, long, geoHashPrecision)

	assert.Equal(t, resp.Temperature, 31.4)
	assert.Equal(t, resp.Humidity, 52.0)
	assert.Equal(t, resp.Elevation, 12.5)
	assert.True(t, state.execCalled)
	assert.Equal(t, state.execArgs[0].Value.(string), wantGeohash)
	assert.Equal(t, state.execArgs[1].Value.(float64), 31.4)
	assert.Equal(t, state.execArgs[2].Value.(float64), 52.0)
	assert.Equal(t, state.execArgs[3].Value.(float64), 12.5)
}

func TestServerGetWeatherOpenMeteoError(t *testing.T) {
	state := &testDBState{queryErr: sql.ErrNoRows}
	srv := &server{queries: newTestQueries(t, state)}

	setTestHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusServiceUnavailable, "try again later"), nil
	})

	resp, err := srv.GetWeather(context.Background(), &weatherv1.GetWeatherRequest{
		Latitude:  25.2048,
		Longitude: 55.2708,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	assert.Nil(t, resp)
	assert.Equal(t, state.execCalled, false)
}
