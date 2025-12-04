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
	Uptime        time.Duration `json:"uptime"`
	startTime     time.Time
}

// SimulationState maintains the state of drift simulation
type SimulationState struct {
	isDrifting    bool
	driftDuration int
	currentOffset float64
	targetOffset  float64
}

var (
	currentStatus *SensorStatus
	simState      = &SimulationState{}
)

func main() {
	var (
		sensorID      = flag.String("id", "", "Sensor ID (auto-generated if empty)")
		natsURL       = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		interval      = flag.Duration("interval", 1*time.Second, "Publication interval")
		baseValue     = flag.Float64("base", 50.0, "Base value for readings")
		noiseLevel    = flag.Float64("noise", 2.0, "Noise level (std deviation)") // Reduced noise for stability
		anomalyChance = flag.Float64("anomaly", 0.05, "Probability of Drift (0-1)") // Chance to start drifting
		spikeChance   = flag.Float64("spike", 0.02, "Probability of Spike (0-1)")   // Chance of single huge spike
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
		value := generateValue(rng, *baseValue, *noiseLevel, *anomalyChance, *spikeChance)

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

func generateValue(rng *rand.Rand, base, noise, driftChance, spikeChance float64) float64 {
	// 1. Manage Drift State (Gradual Transitions)
	if simState.isDrifting {
		simState.driftDuration--
		
		// Move currentOffset towards targetOffset (Approach Phase)
		if simState.currentOffset < simState.targetOffset {
			simState.currentOffset += rng.Float64() * 5.0 // Slowly increase
			if simState.currentOffset > simState.targetOffset {
				simState.currentOffset = simState.targetOffset
			}
		} else if simState.currentOffset > simState.targetOffset {
			simState.currentOffset -= rng.Float64() * 5.0 // Slowly decrease
			if simState.currentOffset < simState.targetOffset {
				simState.currentOffset = simState.targetOffset
			}
		}

		if simState.driftDuration <= 0 {
			simState.isDrifting = false
			log.Printf("End of Drift. Returning to normal.")
		}
	} else {
		// Recovery Phase: Slowly return offset to 0
		if simState.currentOffset != 0 {
			approachSpeed := rng.Float64() * 3.0
			if simState.currentOffset > 0 {
				simState.currentOffset -= approachSpeed
				if simState.currentOffset < 0 { simState.currentOffset = 0 }
			} else {
				simState.currentOffset += approachSpeed
				if simState.currentOffset > 0 { simState.currentOffset = 0 }
			}
		}

		// 2. Start new Drift?
		if simState.currentOffset == 0 && rng.Float64() < driftChance {
			simState.isDrifting = true
			simState.driftDuration = rng.Intn(10) + 10 // Longer drift (10-20s)
			
			// Target offset: +/- 35 (aiming for 15 or 85)
			if rng.Float64() < 0.5 {
				simState.targetOffset = -35.0 
			} else {
				simState.targetOffset = 35.0
			}
			log.Printf("Starting Drift! Target: %.2f", simState.targetOffset)
		}
	}

	// 3. Spike Anomaly (Instantaneous)
	// Add spike ON TOP of current state
	spike := 0.0
	if rng.Float64() < spikeChance {
		if rng.Float64() < 0.5 {
			spike = 60.0 + rng.Float64()*20.0 // +60 to +80
		} else {
			spike = -60.0 - rng.Float64()*20.0 // -60 to -80
		}
		log.Printf("Generating Spike! Value: %.2f", base + simState.currentOffset + spike)
	}

	// 4. Calculate Final Value
	// Base + Gradual Offset + Spike + Noise
	return base + simState.currentOffset + spike + rng.NormFloat64()*noise
}
