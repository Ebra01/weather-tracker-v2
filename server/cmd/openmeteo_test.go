package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ebra01/weather-tracker-v2/server/internal/assert"
)

func TestGetWeatherData(t *testing.T) {
	setTestHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, r.Method, http.MethodGet)
		assert.Equal(t, r.URL.Scheme, "https")
		assert.Equal(t, r.URL.Host, "api.open-meteo.com")
		assert.Equal(t, r.URL.Path, "/v1/forecast")
		assert.Equal(t, r.URL.Query().Get("latitude"), "25.2048")
		assert.Equal(t, r.URL.Query().Get("longitude"), "55.2708")
		assert.Equal(t, r.URL.Query().Get("current"), "temperature_2m,relative_humidity_2m")

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

	var result Result
	got, err := FetchCurrentWeather(25.2048, 55.2708, &result)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, got.Temperature, 31.4)
	assert.Equal(t, got.Humidity, 52.0)
	assert.Equal(t, got.Elevation, 12.5)
}

func TestGetWeatherDataError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		err     error
		wantErr string
	}{
		{
			name:    "Transport error",
			err:     errors.New("network unavailable"),
			wantErr: "network unavailable",
		},
		{
			name:    "Non-OK response",
			status:  http.StatusTooManyRequests,
			body:    "rate limited",
			wantErr: "Unable to connect to Open-meteo: Status (429)",
		},
		{
			name:    "Invalid JSON",
			status:  http.StatusOK,
			body:    "{",
			wantErr: "unexpected end of JSON input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestHTTPClient(t, func(r *http.Request) (*http.Response, error) {
				if tt.err != nil {
					return nil, tt.err
				}

				return testHTTPResponse(tt.status, tt.body), nil
			})

			var result Result
			got, err := FetchCurrentWeather(25.2048, 55.2708, &result)
			if err == nil {
				t.Fatal("expected error")
			}

			assert.True(t, strings.Contains(err.Error(), tt.wantErr))
			assert.Equal(t, got.Temperature, 0.0)
			assert.Equal(t, got.Humidity, 0.0)
			assert.Equal(t, got.Elevation, 0.0)
		})
	}
}
