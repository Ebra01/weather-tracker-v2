package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ebra01/weather-tracker-v2/client/internal/assert"

	api "github.com/ebra01/weather-tracker-v2/client/cmd/api"

	weatherv1 "github.com/ebra01/weather-tracker-v2/pb/weather/v1"

	"google.golang.org/grpc"
)

type stubWeatherServiceClient struct {
	response *weatherv1.GetWeatherResponse
	err      error
	gotReq   *weatherv1.GetWeatherRequest
}

func (s *stubWeatherServiceClient) GetWeather(ctx context.Context, in *weatherv1.GetWeatherRequest, opts ...grpc.CallOption) (*weatherv1.GetWeatherResponse, error) {
	s.gotReq = in
	return s.response, s.err
}

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
			key:      "GRPC_ADDR",
			value:    "server:50051",
			fallback: "localhost:50051",
			want:     "server:50051",
		},
		{
			name:     "Fallback value",
			key:      "GRPC_ADDR",
			fallback: "localhost:50051",
			want:     "localhost:50051",
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

func TestWeatherRoute(t *testing.T) {
	tests := []struct {
		name          string
		urlPath       string
		wantStatus    int
		wantBody      string
		wantGRPCCall  bool
		wantLatitude  float64
		wantLongitude float64
	}{
		{
			name:          "Valid request",
			urlPath:       "/weather?lat=25.2048&lon=55.2708",
			wantStatus:    http.StatusOK,
			wantGRPCCall:  true,
			wantLatitude:  25.2048,
			wantLongitude: 55.2708,
		},
		{
			name:       "Missing latitude",
			urlPath:    "/weather?lon=55.2708",
			wantStatus: http.StatusBadRequest,
			wantBody:   "Query argument lat is required",
		},
		{
			name:       "Invalid longitude",
			urlPath:    "/weather?lat=25.2048&lon=west",
			wantStatus: http.StatusBadRequest,
			wantBody:   "Invalid format for parameter lon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcClient := &stubWeatherServiceClient{
				response: &weatherv1.GetWeatherResponse{
					Temperature: 31.4,
					Humidity:    52,
					Elevation:   12.5,
				},
			}
			handler := api.Handler(NewWeatherClient(grpcClient))

			req := httptest.NewRequest(http.MethodGet, tt.urlPath, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			res := rr.Result()
			defer res.Body.Close()

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, res.StatusCode, tt.wantStatus)
			assert.True(t, strings.Contains(string(body), tt.wantBody))

			if tt.wantGRPCCall {
				if grpcClient.gotReq == nil {
					t.Fatal("expected gRPC GetWeather to be called")
				}

				assert.Equal(t, grpcClient.gotReq.Latitude, tt.wantLatitude)
				assert.Equal(t, grpcClient.gotReq.Longitude, tt.wantLongitude)
			} else {
				assert.Nil(t, grpcClient.gotReq)
			}
		})
	}
}

func TestWeatherClientGetWeather(t *testing.T) {
	grpcClient := &stubWeatherServiceClient{
		response: &weatherv1.GetWeatherResponse{
			Temperature: 31.4,
			Humidity:    52,
			Elevation:   12.5,
		},
	}
	weatherClient := NewWeatherClient(grpcClient)

	req := httptest.NewRequest(http.MethodGet, "/weather?lat=25.2048&lon=55.2708", nil)
	rr := httptest.NewRecorder()

	weatherClient.GetWeather(rr, req, api.GetWeatherParams{Lat: 25.2048, Lon: 55.2708})

	res := rr.Result()
	defer res.Body.Close()

	var body api.WeatherResponse
	err := json.NewDecoder(res.Body).Decode(&body)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, res.StatusCode, http.StatusOK)
	assert.Equal(t, res.Header.Get("Content-Type"), "application/json")
	assert.Equal(t, *body.Temperature, 31.4)
	assert.Equal(t, *body.Humidity, 52.0)
	assert.Equal(t, *body.Elevation, 12.5)
	assert.Equal(t, grpcClient.gotReq.Latitude, 25.2048)
	assert.Equal(t, grpcClient.gotReq.Longitude, 55.2708)
}

func TestWeatherClientGetWeatherBackendError(t *testing.T) {
	grpcClient := &stubWeatherServiceClient{
		err: errors.New("backend unavailable"),
	}
	weatherClient := NewWeatherClient(grpcClient)

	req := httptest.NewRequest(http.MethodGet, "/weather?lat=25.2048&lon=55.2708", nil)
	rr := httptest.NewRecorder()

	weatherClient.GetWeather(rr, req, api.GetWeatherParams{Lat: 25.2048, Lon: 55.2708})

	res := rr.Result()
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, res.StatusCode, http.StatusInternalServerError)
	assert.True(t, strings.Contains(string(body), "Failed to fetch weather data from backend service"))
	assert.Equal(t, grpcClient.gotReq.Latitude, 25.2048)
	assert.Equal(t, grpcClient.gotReq.Longitude, 55.2708)
}
