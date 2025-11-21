package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type SensorReading struct {
	SensorID  string  `json:"sensor_id"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
}

func main() {
	var (
		sensorID      = flag.String("id", "", "Sensor ID (auto-generated if empty)")
		natsURL       = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		interval      = flag.Duration("interval", 1*time.Second, "Publication interval")
		baseValue     = flag.Float64("base", 50.0, "Base value for readings")
		noiseLevel    = flag.Float64("noise", 5.0, "Noise level (std deviation)")
		anomalyChance = flag.Float64("anomaly", 0.0, "Probability of anomaly (0-1)")
	)
	flag.Parse()

	// Generate sensor ID if not provided
	if *sensorID == "" {
		*sensorID = "sensor-" + uuid.New().String()[:8]
	}

	// Connect to NATS
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	log.Printf("Sensor %s started, publishing to sensors.readings every %v", *sensorID, *interval)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for range ticker.C {
		value := generateValue(rng, *baseValue, *noiseLevel, *anomalyChance)
		
		reading := SensorReading{
			SensorID:  *sensorID,
			Value:     value,
			Timestamp: time.Now().Unix(),
		}

		data, err := json.Marshal(reading)
		if err != nil {
			log.Printf("Error marshaling reading: %v", err)
			continue
		}

		if err := nc.Publish("sensors.readings", data); err != nil {
			log.Printf("Error publishing reading: %v", err)
			continue
		}

		log.Printf("Published: sensor_id=%s, value=%.2f, timestamp=%d", reading.SensorID, reading.Value, reading.Timestamp)
	}
}

func generateValue(rng *rand.Rand, base, noise, anomalyChance float64) float64 {
	// Generate base value with normal distribution noise
	value := base + rng.NormFloat64()*noise

	// Occasionally inject anomaly
	if rng.Float64() < anomalyChance {
		// Anomaly: either very high or very low
		if rng.Float64() < 0.5 {
			value = base + 100.0 + rng.Float64()*50.0 // High anomaly
		} else {
			value = base - 100.0 - rng.Float64()*50.0 // Low anomaly
		}
	}

	return value
}

