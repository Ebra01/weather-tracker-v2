package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	client "weather-tracker/client/cmd/api"
	weatherv1 "weather-tracker/pb/weather/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type WeatherClient struct {
	grpcClient weatherv1.WeatherServiceClient
}

func NewWeatherClient(client weatherv1.WeatherServiceClient) *WeatherClient {
	return &WeatherClient{
		grpcClient: client,
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (s *WeatherClient) GetWeather(w http.ResponseWriter, r *http.Request, params client.GetWeatherParams) {

	var (
		temperature float64
		humidity    float64
		elevation   float64
	)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	grpcReq := &weatherv1.GetWeatherRequest{
		Latitude:  params.Lat,
		Longitude: params.Lon,
	}

	grpcResp, err := s.grpcClient.GetWeather(ctx, grpcReq)
	if err != nil {
		log.Printf("[GET] /weather?lat=%f&lon=%f - Error: %v", params.Lat, params.Lon, err)
		http.Error(w, "Failed to fetch weather data from backend service", http.StatusInternalServerError)
		return
	}

	log.Printf("[GET] /weather?lat=%f&lon=%f - OK - T: %.2f, H: %.2f, E: %.2f", params.Lat, params.Lon, grpcResp.Temperature, grpcResp.Humidity, grpcResp.Elevation)

	temperature = grpcResp.Temperature
	humidity = grpcResp.Humidity
	elevation = grpcResp.Elevation

	responseBody := client.WeatherResponse{
		Temperature: &temperature,
		Humidity:    &humidity,
		Elevation:   &elevation,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(responseBody)

}

func main() {

	grpcAddr := envOrDefault("GRPC_ADDR", "localhost:50051")

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Did not connect to gRPC server: %v", err)
	}

	defer conn.Close()

	grpcClient := weatherv1.NewWeatherServiceClient(conn)

	weatherService := NewWeatherClient(grpcClient)

	mux := http.NewServeMux()
	client.HandlerFromMux(weatherService, mux)

	log.Println("API Gateway listening on :8080...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Could not start server: %v", err)
	}

}
