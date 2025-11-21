package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

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

type GlobalStats struct {
	mu              sync.RWMutex
	readings        []float64
	alerts          []Alert
	edgeNodes       map[string]int
	totalReadings   int
	sum             float64
	min             float64
	max             float64
	startTime       time.Time
	latencies       []time.Duration
}

func main() {
	var (
		natsURL      = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		statsInterval = flag.Duration("stats", 10*time.Second, "Statistics reporting interval")
		maxReadings   = flag.Int("max-readings", 10000, "Maximum readings to keep in memory")
	)
	flag.Parse()

	// Connect to NATS
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	log.Println("Cloud Processor started, listening to edge.*")

	stats := &GlobalStats{
		readings:  make([]float64, 0, *maxReadings),
		alerts:    make([]Alert, 0),
		edgeNodes: make(map[string]int),
		min:       math.Inf(1),
		max:       math.Inf(-1),
		startTime: time.Now(),
		latencies: make([]time.Duration, 0),
	}

	// Subscribe to filtered readings
	_, err = nc.Subscribe("edge.filtered", func(msg *nats.Msg) {
		var filtered FilteredReading
		if err := json.Unmarshal(msg.Data, &filtered); err != nil {
			// Might be an aggregate, try parsing as map
			var agg map[string]interface{}
			if err2 := json.Unmarshal(msg.Data, &agg); err2 == nil {
				processAggregate(agg, stats)
			}
			return
		}
		processFilteredReading(filtered, stats)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to edge.filtered: %v", err)
	}

	// Subscribe to alerts
	_, err = nc.Subscribe("edge.alerts", func(msg *nats.Msg) {
		var alert Alert
		if err := json.Unmarshal(msg.Data, &alert); err != nil {
			log.Printf("Error unmarshaling alert: %v", err)
			return
		}
		processAlert(alert, stats)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to edge.alerts: %v", err)
	}

	// Start statistics reporter
	ticker := time.NewTicker(*statsInterval)
	defer ticker.Stop()

	for range ticker.C {
		stats.report()
	}
}

func processFilteredReading(reading FilteredReading, stats *GlobalStats) {
	now := time.Now().Unix()
	latency := time.Duration(now-reading.Timestamp) * time.Second

	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.totalReadings++
	stats.sum += reading.Value
	if reading.Value < stats.min {
		stats.min = reading.Value
	}
	if reading.Value > stats.max {
		stats.max = reading.Value
	}

	// Keep readings in sliding window
	if len(stats.readings) >= cap(stats.readings) {
		stats.readings = stats.readings[1:]
	}
	stats.readings = append(stats.readings, reading.Value)

	// Track edge nodes
	stats.edgeNodes[reading.EdgeID]++

	// Track latencies
	stats.latencies = append(stats.latencies, latency)
	if len(stats.latencies) > 10000 {
		stats.latencies = stats.latencies[1:]
	}
}

func processAggregate(agg map[string]interface{}, stats *GlobalStats) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if edgeID, ok := agg["edge_id"].(string); ok {
		if count, ok := agg["count"].(float64); ok {
			stats.edgeNodes[edgeID] += int(count)
		}
	}
}

func processAlert(alert Alert, stats *GlobalStats) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.alerts = append(stats.alerts, alert)
	if len(stats.alerts) > 1000 {
		stats.alerts = stats.alerts[1:]
	}

	log.Printf("Alert received: sensor_id=%s, edge_id=%s, value=%.2f, message=%s",
		alert.SensorID, alert.EdgeID, alert.Value, alert.Message)
}

func (s *GlobalStats) report() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.totalReadings == 0 {
		log.Println("No readings received yet")
		return
	}

	mean := s.sum / float64(s.totalReadings)
	
	// Calculate standard deviation
	var variance float64
	for _, v := range s.readings {
		variance += (v - mean) * (v - mean)
	}
	stdDev := math.Sqrt(variance / float64(len(s.readings)))

	// Calculate percentiles for latency
	latencyP95 := calculatePercentile(s.latencies, 95)
	latencyP99 := calculatePercentile(s.latencies, 99)
	var avgLatency time.Duration
	if len(s.latencies) > 0 {
		var sum time.Duration
		for _, l := range s.latencies {
			sum += l
		}
		avgLatency = sum / time.Duration(len(s.latencies))
	}

	uptime := time.Since(s.startTime)
	rate := float64(s.totalReadings) / uptime.Seconds()

	log.Println("=== GLOBAL STATISTICS ===")
	log.Printf("Uptime: %v", uptime)
	log.Printf("Total Readings: %d", s.totalReadings)
	log.Printf("Readings/sec: %.2f", rate)
	log.Printf("Mean: %.2f", mean)
	log.Printf("Std Dev: %.2f", stdDev)
	log.Printf("Min: %.2f", s.min)
	log.Printf("Max: %.2f", s.max)
	log.Printf("Active Edge Nodes: %d", len(s.edgeNodes))
	log.Printf("Total Alerts: %d", len(s.alerts))
	log.Printf("Latency - Avg: %v, P95: %v, P99: %v", avgLatency, latencyP95, latencyP99)
	
	// Edge node breakdown
	for edgeID, count := range s.edgeNodes {
		log.Printf("  Edge %s: %d readings", edgeID, count)
	}
	log.Println("=========================")
}

func calculatePercentile(latencies []time.Duration, p int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	// Create a copy and sort
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	index := int(float64(len(sorted)) * float64(p) / 100.0)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

