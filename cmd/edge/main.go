package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math"
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
	count         int
	sum           float64
	min           float64
	max           float64
	windowValues  []float64
	windowSize    int
	lastAggregate time.Time
}

func main() {
	var (
		edgeID       = flag.String("id", "", "Edge Node ID (auto-generated if empty)")
		natsURL      = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		thresholdMin = flag.Float64("min", 0.0, "Minimum threshold for alerts")
		thresholdMax = flag.Float64("max", 200.0, "Maximum threshold for alerts")
		noiseFilter  = flag.Float64("noise", 3.0, "Noise filter threshold (std deviations)")
		windowSize   = flag.Int("window", 10, "Aggregation window size")
		aggregateInt = flag.Duration("aggregate", 5*time.Second, "Aggregation interval")
		useJetStream = flag.Bool("jetstream", false, "Use JetStream for persistence")
	)
	flag.Parse()

	// Generate edge ID if not provided
	if *edgeID == "" {
		*edgeID = "edge-" + time.Now().Format("20060102-150405")
	}

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

	stats := &EdgeStats{
		windowValues: make([]float64, 0, *windowSize),
		windowSize:   *windowSize,
		min:          math.Inf(1),
		max:          math.Inf(-1),
	}

	// Start aggregation timer
	go func() {
		ticker := time.NewTicker(*aggregateInt)
		defer ticker.Stop()
		for range ticker.C {
			stats.publishAggregate(nc, *edgeID)
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
				
				processMessage(msg.Data(), stats, nc, *edgeID, *thresholdMin, *thresholdMax, *noiseFilter)
				if err := msg.Ack(); err != nil {
					log.Printf("Error acking message: %v", err)
				}
			}
		}()

		// Keep running
		select {}
	} else {
		sub, err = nc.Subscribe("sensors.readings", func(msg *nats.Msg) {
			processMessage(msg.Data, stats, nc, *edgeID, *thresholdMin, *thresholdMax, *noiseFilter)
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

func processMessage(data []byte, stats *EdgeStats, nc *nats.Conn, edgeID string, thresholdMin, thresholdMax, noiseFilter float64) {
	var reading SensorReading
	if err := json.Unmarshal(data, &reading); err != nil {
		log.Printf("Error unmarshaling reading: %v", err)
		return
	}

	// Update statistics
	stats.mu.Lock()
	stats.count++
	stats.sum += reading.Value
	if reading.Value < stats.min {
		stats.min = reading.Value
	}
	if reading.Value > stats.max {
		stats.max = reading.Value
	}
	stats.windowValues = append(stats.windowValues, reading.Value)
	if len(stats.windowValues) > stats.windowSize {
		stats.windowValues = stats.windowValues[1:]
	}
	mean := stats.sum / float64(stats.count)
	stats.mu.Unlock()

	// Calculate standard deviation for noise filtering
	var stdDev float64
	if len(stats.windowValues) > 1 {
		var variance float64
		for _, v := range stats.windowValues {
			variance += (v - mean) * (v - mean)
		}
		stdDev = math.Sqrt(variance / float64(len(stats.windowValues)))
	}

	// Noise filtering: reject if value is too far from mean
	if stdDev > 0 && math.Abs(reading.Value-mean) > noiseFilter*stdDev {
		log.Printf("Filtered out noise: sensor_id=%s, value=%.2f, mean=%.2f, std=%.2f", 
			reading.SensorID, reading.Value, mean, stdDev)
		return
	}

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
	if reading.Value < thresholdMin || reading.Value > thresholdMax {
		alert := Alert{
			SensorID:  reading.SensorID,
			Value:     reading.Value,
			Timestamp: reading.Timestamp,
			EdgeID:    edgeID,
			Type:      "threshold",
			Message:   "",
		}

		if reading.Value < thresholdMin {
			alert.Message = "Value below minimum threshold"
		} else {
			alert.Message = "Value above maximum threshold"
		}

		alertData, err := json.Marshal(alert)
		if err != nil {
			log.Printf("Error marshaling alert: %v", err)
			return
		}

		if err := nc.Publish("edge.alerts", alertData); err != nil {
			log.Printf("Error publishing alert: %v", err)
		}

		log.Printf("Alert published: sensor_id=%s, value=%.2f, %s", reading.SensorID, reading.Value, alert.Message)
	}
}

func (s *EdgeStats) publishAggregate(nc *nats.Conn, edgeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.count == 0 {
		return
	}

	mean := s.sum / float64(s.count)
	
	aggregate := map[string]interface{}{
		"edge_id":   edgeID,
		"count":     s.count,
		"mean":      mean,
		"min":       s.min,
		"max":       s.max,
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
		edgeID, s.count, mean, s.min, s.max)

	// Reset stats
	s.count = 0
	s.sum = 0
	s.min = math.Inf(1)
	s.max = math.Inf(-1)
	s.windowValues = s.windowValues[:0]
}

