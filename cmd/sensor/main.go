package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type SensorReading struct {
	SensorID  string  `json:"sensor_id"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
}

type SensorStatus struct {
	mu            sync.RWMutex
	SensorID      string    `json:"sensor_id"`
	Status        string    `json:"status"`
	LastReading   *SensorReading `json:"last_reading,omitempty"`
	TotalReadings int64     `json:"total_readings"`
	Uptime        time.Duration `json:"uptime"` // This will be calculated on request
	startTime     time.Time
}

var currentStatus *SensorStatus

func main() {
	var (
		sensorID      = flag.String("id", "", "Sensor ID (auto-generated if empty)")
		natsURL       = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		interval      = flag.Duration("interval", 1*time.Second, "Publication interval")
		baseValue     = flag.Float64("base", 50.0, "Base value for readings")
		noiseLevel    = flag.Float64("noise", 5.0, "Noise level (std deviation)")
		anomalyChance = flag.Float64("anomaly", 0.0, "Probability of anomaly (0-1)")
		httpPort      = flag.String("http-port", "8081", "HTTP API port")
	)
	flag.Parse()

	// Generate sensor ID if not provided
	if *sensorID == "" {
		*sensorID = "sensor-" + uuid.New().String()[:8]
	}

	// Initialize Status
	currentStatus = &SensorStatus{
		SensorID:  *sensorID,
		Status:    "Starting",
		startTime: time.Now(),
	}

	// Start HTTP Server
	go startAPIServer(*httpPort)

	// Connect to NATS
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	updateStatus("Running", nil)
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
			updateStatus("Error Publishing", &reading)
			continue
		}

		updateStatus("Running", &reading)
		log.Printf("Published: sensor_id=%s, value=%.2f, timestamp=%d", reading.SensorID, reading.Value, reading.Timestamp)
	}
}

func updateStatus(status string, reading *SensorReading) {
	currentStatus.mu.Lock()
	defer currentStatus.mu.Unlock()
	
	currentStatus.Status = status
	if reading != nil {
		currentStatus.LastReading = reading
		currentStatus.TotalReadings++
	}
}

func startAPIServer(port string) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		currentStatus.mu.RLock()
		defer currentStatus.mu.RUnlock()

		// Create a display struct to handle calculated fields
		type DisplayStatus struct {
			*SensorStatus
			UptimeString string `json:"uptime_string"`
		}
		
		display := DisplayStatus{
			SensorStatus: currentStatus,
			UptimeString: time.Since(currentStatus.startTime).String(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(display)
	})

	log.Printf("Starting HTTP API on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("HTTP Server failed: %v", err)
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
