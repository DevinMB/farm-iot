package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/segmentio/kafka-go"

	"github.com/farmsense/consumer/internal/config"
)

// SensorReading is the expected message schema from hubs.
type SensorReading struct {
	NodeID     string  `json:"node_id"`
	FarmID     string  `json:"farm_id"`
	SensorType string  `json:"sensor_type"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Timestamp  int64   `json:"timestamp"` // Unix epoch seconds
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		slog.Info("shutting down consumer...")
		cancel()
	}()

	slog.Info("loading config from Vault...")
	cfg, err := config.Load(ctx)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	slog.Info("config loaded", "topic", cfg.KafkaTopic, "group", cfg.KafkaGroupID)

	influxClient := influxdb2.NewClient(cfg.InfluxURL, cfg.InfluxToken)
	defer influxClient.Close()

	writeAPI := influxClient.WriteAPIBlocking(cfg.InfluxOrg, cfg.InfluxBucket)

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{cfg.KafkaBrokers},
		Topic:   cfg.KafkaTopic,
		GroupID: cfg.KafkaGroupID,
	})
	defer r.Close()

	slog.Info("consumer started", "topic", cfg.KafkaTopic, "group", cfg.KafkaGroupID)

	for {
		msg, err := r.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Error("kafka read error", "err", err)
			continue
		}

		var reading SensorReading
		if err := json.Unmarshal(msg.Value, &reading); err != nil {
			slog.Warn("invalid message", "err", err, "raw", string(msg.Value))
			continue
		}

		if err := writeToInflux(ctx, writeAPI, reading); err != nil {
			slog.Error("influx write error", "err", err)
		} else {
			slog.Info("wrote reading", "node", reading.NodeID, "sensor", reading.SensorType, "value", reading.Value)
		}
	}
}

func writeToInflux(ctx context.Context, writeAPI api.WriteAPIBlocking, r SensorReading) error {
	p := influxdb2.NewPointWithMeasurement("sensor_reading").
		AddTag("node_id", r.NodeID).
		AddTag("farm_id", r.FarmID).
		AddTag("sensor_type", r.SensorType).
		AddTag("unit", r.Unit).
		AddField("value", r.Value).
		SetTime(time.Unix(r.Timestamp, 0))

	return writeAPI.WritePoint(ctx, p)
}
