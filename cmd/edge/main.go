package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type SensorReading struct {
	SensorID  string  `json:"sensor_id"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
}

type FilteredReading struct {
	SensorID  string  `json:"sensor_id"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
	EdgeID    string  `json:"edge_id"`
}

type Alert struct {
	SensorID  string  `json:"sensor_id"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
	EdgeID    string  `json:"edge_id"`
	Type      string  `json:"type"`
	Message   string  `json:"message"`
}

type EdgeStats struct {
	mu            sync.RWMutex
	Count         int       `json:"count"`
	Sum           float64   `json:"sum"`
	Min           float64   `json:"min"`
	Max           float64   `json:"max"`
	WindowValues  []float64 `json:"-"`
	WindowSize    int       `json:"window_size"`
	LastAggregate time.Time `json:"last_aggregate"`
	EdgeID        string    `json:"edge_id"`
	StartTime     time.Time `json:"start_time"`
}

var globalStats *EdgeStats

func main() {
	var (
		edgeID       = flag.String("id", "", "Edge Node ID (auto-generated if empty)")
		natsURL      = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		thresholdMin = flag.Float64("min", 30.0, "Minimum threshold for alerts")
		thresholdMax = flag.Float64("max", 80.0, "Maximum threshold for alerts")
		noiseFilter  = flag.Float64("noise", 3.0, "Noise filter threshold (std deviations)")
		windowSize   = flag.Int("window", 10, "Aggregation window size")
		aggregateInt = flag.Duration("aggregate", 5*time.Second, "Aggregation interval")
		useJetStream = flag.Bool("jetstream", false, "Use JetStream for persistence")
		httpPort     = flag.String("http-port", "8082", "HTTP API port")
	)
	flag.Parse()

	// Generate edge ID if not provided
	if *edgeID == "" {
		*edgeID = "edge-" + time.Now().Format("20060102-150405")
	}

	// Initialize Stats
	globalStats = &EdgeStats{
		WindowValues: make([]float64, 0, *windowSize),
		WindowSize:   *windowSize,
		Min:          math.Inf(1),
		Max:          math.Inf(-1),
		EdgeID:       *edgeID,
		StartTime:    time.Now(),
	}

	// Start HTTP Server
	go startAPIServer(*httpPort)

	// Connect to NATS
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	var js jetstream.JetStream
	if *useJetStream {
		js, err = jetstream.New(nc)
		if err != nil {
			log.Fatalf("Failed to create JetStream context: %v", err)
		}

		// Create stream if it doesn't exist
		ctx := context.Background()
		_, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "SENSORS",
			Subjects: []string{"sensors.readings"},
			Replicas: 1,
		})
		if err != nil && err.Error() != "stream name already in use" {
			log.Printf("Error creating stream (may already exist): %v", err)
		}
	}

	log.Printf("Edge Node %s started, listening to sensors.readings", *edgeID)

	// Start aggregation timer
	go func() {
		ticker := time.NewTicker(*aggregateInt)
		defer ticker.Stop()
		for range ticker.C {
			globalStats.publishAggregate(nc, *edgeID)
		}
	}()

	// Subscribe to sensor readings
	var sub *nats.Subscription
	if *useJetStream && js != nil {
		ctx := context.Background()
		consumer, err := js.CreateOrUpdateConsumer(ctx, "SENSORS", jetstream.ConsumerConfig{
			Durable:   "EDGE-" + *edgeID,
			AckPolicy: jetstream.AckExplicitPolicy,
		})
		if err != nil {
			log.Fatalf("Failed to create consumer: %v", err)
		}

		msgs, err := consumer.Messages()
		if err != nil {
			log.Fatalf("Failed to get messages: %v", err)
		}

		// Process messages in a goroutine
		go func() {
			for {
				msg, err := msgs.Next()
				if err != nil {
					log.Printf("Error getting next message: %v", err)
					continue
				}
				
				processMessage(msg.Data(), globalStats, nc, *edgeID, *thresholdMin, *thresholdMax, *noiseFilter)
				if err := msg.Ack(); err != nil {
					log.Printf("Error acking message: %v", err)
				}
			}
		}()

		// Keep running
		select {}
	} else {
		sub, err = nc.Subscribe("sensors.readings", func(msg *nats.Msg) {
			processMessage(msg.Data, globalStats, nc, *edgeID, *thresholdMin, *thresholdMax, *noiseFilter)
		})
		if err != nil {
			log.Fatalf("Failed to subscribe: %v", err)
		}

		// Keep running
		select {}
	}

	if sub != nil {
		sub.Unsubscribe()
	}
}

func startAPIServer(port string) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		globalStats.mu.RLock()
		defer globalStats.mu.RUnlock()

		// Display struct
		type DisplayStats struct {
			*EdgeStats
			Mean float64 `json:"mean"`
			Uptime string `json:"uptime"`
		}

		mean := 0.0
		if globalStats.Count > 0 {
			mean = globalStats.Sum / float64(globalStats.Count)
		}

		display := DisplayStats{
			EdgeStats: globalStats,
			Mean:      mean,
			Uptime:    time.Since(globalStats.StartTime).String(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(display)
	})

	log.Printf("Starting HTTP API on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("HTTP Server failed: %v", err)
	}
}

func processMessage(data []byte, stats *EdgeStats, nc *nats.Conn, edgeID string, thresholdMin, thresholdMax, noiseFilter float64) {
	var reading SensorReading
	if err := json.Unmarshal(data, &reading); err != nil {
		log.Printf("Error unmarshaling reading: %v", err)
		return
	}

	// Update statistics
	stats.mu.Lock()
	stats.Count++
	stats.Sum += reading.Value
	if reading.Value < stats.Min {
		stats.Min = reading.Value
	}
	if reading.Value > stats.Max {
		stats.Max = reading.Value
	}
	stats.WindowValues = append(stats.WindowValues, reading.Value)
	if len(stats.WindowValues) > stats.WindowSize {
		stats.WindowValues = stats.WindowValues[1:]
	}
	// mean := stats.Sum / float64(stats.Count) // Not used currently
	stats.mu.Unlock()

	// Calculate standard deviation for noise filtering (just for logging if needed)
	/*
	var stdDev float64
	if len(stats.WindowValues) > 1 {
		var variance float64
		for _, v := range stats.WindowValues {
			variance += (v - mean) * (v - mean)
		}
		stdDev = math.Sqrt(variance / float64(len(stats.WindowValues)))
	}

	// Noise filtering disabled to allow drift detection
	if stdDev > 0 && math.Abs(reading.Value-mean) > noiseFilter*stdDev {
		log.Printf("Potential noise detected (kept): sensor_id=%s, value=%.2f, mean=%.2f, std=%.2f", 
			reading.SensorID, reading.Value, mean, stdDev)
	}
	*/

	// Create filtered reading
	filtered := FilteredReading{
		SensorID:  reading.SensorID,
		Value:     reading.Value,
		Timestamp: reading.Timestamp,
		EdgeID:    edgeID,
	}

	filteredData, err := json.Marshal(filtered)
	if err != nil {
		log.Printf("Error marshaling filtered reading: %v", err)
		return
	}

	// Publish filtered reading
	if err := nc.Publish("edge.filtered", filteredData); err != nil {
		log.Printf("Error publishing filtered reading: %v", err)
	}

	// Check for threshold violations
	// Critical range: < 0 or > 100 (Spikes)
	// Warning range: < 40 or > 60 (Drift)
	
	alertType := ""
	alertMsg := ""
	
	if reading.Value < 0 || reading.Value > 100 {
		alertType = "critical"
		alertMsg = "Critical value detected (Spike)"
	} else if reading.Value < 40 || reading.Value > 60 {
		alertType = "warning"
		alertMsg = "Process drift detected (Warning)"
	}

	if alertType != "" {
		alert := Alert{
			SensorID:  reading.SensorID,
			Value:     reading.Value,
			Timestamp: reading.Timestamp,
			EdgeID:    edgeID,
			Type:      alertType,
			Message:   alertMsg,
		}

		alertData, err := json.Marshal(alert)
		if err != nil {
			log.Printf("Error marshaling alert: %v", err)
			return
		}

		if err := nc.Publish("edge.alerts", alertData); err != nil {
			log.Printf("Error publishing alert: %v", err)
		}

		log.Printf("Alert published [%s]: sensor_id=%s, value=%.2f, %s", alertType, reading.SensorID, reading.Value, alertMsg)
	}
}

func (s *EdgeStats) publishAggregate(nc *nats.Conn, edgeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Count == 0 {
		return
	}

	mean := s.Sum / float64(s.Count)
	
	aggregate := map[string]interface{}{
		"edge_id":   edgeID,
		"count":     s.Count,
		"mean":      mean,
		"min":       s.Min,
		"max":       s.Max,
		"timestamp": time.Now().Unix(),
	}

	data, err := json.Marshal(aggregate)
	if err != nil {
		log.Printf("Error marshaling aggregate: %v", err)
		return
	}

	if err := nc.Publish("edge.filtered", data); err != nil {
		log.Printf("Error publishing aggregate: %v", err)
	}

	log.Printf("Aggregate published: edge_id=%s, count=%d, mean=%.2f, min=%.2f, max=%.2f", 
		edgeID, s.Count, mean, s.Min, s.Max)

	// Reset stats
	s.Count = 0
	s.Sum = 0
	s.Min = math.Inf(1)
	s.Max = math.Inf(-1)
	s.WindowValues = s.WindowValues[:0]
}

