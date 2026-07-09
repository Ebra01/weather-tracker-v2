package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"weather-tracker/server/internal/weatherdb"

	weatherv1 "github.com/ebra01/weather-tracker-v2/pb/weather/v1"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mmcloughlin/geohash"
	"google.golang.org/grpc"
)

// 5 characters of geohash gives ~4.9km precision, which is suitable for weather data.
const geoHashPrecision = 5

type Result struct {
	Geohash     string
	Temperature float64
	Humidity    float64
	Elevation   float64
}

type server struct {
	weatherv1.UnimplementedWeatherServiceServer
	queries *weatherdb.Queries
}

func openDB(connString string) (*sql.DB, error) {

	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil

}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func weatherResponseFromResult(result Result) *weatherv1.GetWeatherResponse {
	return &weatherv1.GetWeatherResponse{
		Temperature: float64(result.Temperature),
		Humidity:    float64(result.Humidity),
		Elevation:   float64(result.Elevation),
	}
}

func (s *server) GetDataFromCache(ctx context.Context, lat, long float64) (Result, bool, error) {

	hash := geohash.EncodeWithPrecision(lat, long, geoHashPrecision)

	row, err := s.queries.GetWeather(ctx, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{Geohash: hash}, false, nil
		}
		return Result{Geohash: hash}, false, err
	}

	return Result{
		Geohash:     hash,
		Temperature: row.Temperature,
		Humidity:    row.Humidity,
		Elevation:   row.Elevation,
	}, true, nil

}

func (s *server) SaveTemperature(ctx context.Context, result Result) error {

	return s.queries.SetWeather(ctx, weatherdb.SetWeatherParams{
		Geohash:     result.Geohash,
		Temperature: result.Temperature,
		Humidity:    result.Humidity,
		Elevation:   result.Elevation,
	})
}

func (s *server) GetWeather(ctx context.Context, in *weatherv1.GetWeatherRequest) (*weatherv1.GetWeatherResponse, error) {

	var result Result
	// Check the database to get the value.
	result, ok, err := s.GetDataFromCache(ctx, in.Latitude, in.Longitude)
	if ok {
		log.Println("Cache Hit - Retrieving data from database...")
		return weatherResponseFromResult(result), nil
	}

	var errMsg string

	if err != nil {
		errMsg = fmt.Sprintf("Failed to get data from database: %v - (Defaulting to OpenMeteo API)\n", err)
	} else {
		errMsg = "No data found in database - (Defaulting to OpenMeteo API)\n"
	}

	// If not available in database - get the value from OpenMeteo
	log.Printf("Cache Miss - %s", errMsg)

	result, err = FetchCurrentWeather(in.Latitude, in.Longitude, &result)
	if err != nil {
		log.Printf("Unable to get data from OpenMeteo - try again in a few moments: %v\n", err)
		return nil, err
	}

	// Save new value to database
	err = s.SaveTemperature(ctx, result)
	if err != nil {
		log.Printf("Failed to save result to database %v\n", err)
	}

	log.Printf("Request Temperature for Latitude (%v) and Longitude (%v) - Got Tempreture %.2f C, Humidity %.2f, & Elevation %.1fm (above sea level)\n", in.Latitude, in.Longitude, result.Temperature, result.Humidity, result.Elevation)

	return weatherResponseFromResult(result), nil
}

func main() {

	connStr := envOrDefault("DATABASE_URL", "postgres://web:pass@localhost:5432/weatherapp?sslmode=disable")

	db, err := openDB(connStr)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
		os.Exit(1)
	}

	log.Println("Connected to PostgreSQL database: weatherapp")

	port := ":50051"

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	queries := weatherdb.New(db)

	weatherv1.RegisterWeatherServiceServer(grpcServer, &server{queries: queries})

	log.Printf("gRPC Weather Server is running on port %s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}

}
