package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
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
	Readings        []float64         `json:"-"`
	Alerts          []Alert           `json:"alerts"`
	EdgeNodes       map[string]int    `json:"edge_nodes"`
	TotalReadings   int               `json:"total_readings"`
	Sum             float64           `json:"sum"`
	Min             float64           `json:"min"`
	Max             float64           `json:"max"`
	StartTime       time.Time         `json:"start_time"`
	Latencies       []time.Duration   `json:"-"`
}

var currentStats *GlobalStats

func main() {
	var (
		natsURL      = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		statsInterval = flag.Duration("stats", 10*time.Second, "Statistics reporting interval")
		maxReadings   = flag.Int("max-readings", 10000, "Maximum readings to keep in memory")
		httpPort      = flag.String("http-port", "8080", "HTTP API port")
	)
	flag.Parse()

	// Connect to NATS
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	log.Println("Cloud Processor started, listening to edge.*")

	currentStats = &GlobalStats{
		Readings:  make([]float64, 0, *maxReadings),
		Alerts:    make([]Alert, 0),
		EdgeNodes: make(map[string]int),
		Min:       math.Inf(1),
		Max:       math.Inf(-1),
		StartTime: time.Now(),
		Latencies: make([]time.Duration, 0),
	}

	// Start HTTP Server
	go startAPIServer(*httpPort)

	// Subscribe to filtered readings
	_, err = nc.Subscribe("edge.filtered", func(msg *nats.Msg) {
		var filtered FilteredReading
		if err := json.Unmarshal(msg.Data, &filtered); err != nil {
			// Might be an aggregate, try parsing as map
			var agg map[string]interface{}
			if err2 := json.Unmarshal(msg.Data, &agg); err2 == nil {
				processAggregate(agg, currentStats)
			}
			return
		}
		processFilteredReading(filtered, currentStats)
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
		processAlert(alert, currentStats)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to edge.alerts: %v", err)
	}

	// Start statistics reporter
	ticker := time.NewTicker(*statsInterval)
	defer ticker.Stop()

	for range ticker.C {
		currentStats.report()
	}
}

func startAPIServer(port string) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		currentStats.mu.RLock()
		defer currentStats.mu.RUnlock()

		type DisplayStats struct {
			*GlobalStats
			Mean           float64 `json:"mean"`
			StdDev         float64 `json:"std_dev"`
			Uptime         string  `json:"uptime"`
			ReadingsPerSec float64 `json:"readings_per_sec"`
			TotalAlerts    int     `json:"total_alerts"`
		}

		mean := 0.0
		if currentStats.TotalReadings > 0 {
			mean = currentStats.Sum / float64(currentStats.TotalReadings)
		}

		var variance float64
		for _, v := range currentStats.Readings {
			variance += (v - mean) * (v - mean)
		}
		stdDev := 0.0
		if len(currentStats.Readings) > 0 {
			stdDev = math.Sqrt(variance / float64(len(currentStats.Readings)))
		}
		
		uptime := time.Since(currentStats.StartTime)
		rate := float64(currentStats.TotalReadings) / uptime.Seconds()

		display := DisplayStats{
			GlobalStats:    currentStats,
			Mean:           mean,
			StdDev:         stdDev,
			Uptime:         uptime.String(),
			ReadingsPerSec: rate,
			TotalAlerts:    len(currentStats.Alerts),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(display)
	})

	log.Printf("Starting HTTP API on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("HTTP Server failed: %v", err)
	}
}

func processFilteredReading(reading FilteredReading, stats *GlobalStats) {
	now := time.Now().Unix()
	latency := time.Duration(now-reading.Timestamp) * time.Second

	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.TotalReadings++
	stats.Sum += reading.Value
	if reading.Value < stats.Min {
		stats.Min = reading.Value
	}
	if reading.Value > stats.Max {
		stats.Max = reading.Value
	}

	// Keep readings in sliding window
	if len(stats.Readings) >= cap(stats.Readings) {
		stats.Readings = stats.Readings[1:]
	}
	stats.Readings = append(stats.Readings, reading.Value)

	// Track edge nodes
	stats.EdgeNodes[reading.EdgeID]++

	// Track latencies
	stats.Latencies = append(stats.Latencies, latency)
	if len(stats.Latencies) > 10000 {
		stats.Latencies = stats.Latencies[1:]
	}
}

func processAggregate(agg map[string]interface{}, stats *GlobalStats) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if edgeID, ok := agg["edge_id"].(string); ok {
		if count, ok := agg["count"].(float64); ok {
			stats.EdgeNodes[edgeID] += int(count)
		}
	}
}

func processAlert(alert Alert, stats *GlobalStats) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.Alerts = append(stats.Alerts, alert)
	if len(stats.Alerts) > 1000 {
		stats.Alerts = stats.Alerts[1:]
	}

	log.Printf("Alert received: sensor_id=%s, edge_id=%s, value=%.2f, message=%s",
		alert.SensorID, alert.EdgeID, alert.Value, alert.Message)
}

func (s *GlobalStats) report() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.TotalReadings == 0 {
		log.Println("No readings received yet")
		return
	}

	mean := s.Sum / float64(s.TotalReadings)
	
	// Calculate standard deviation
	var variance float64
	for _, v := range s.Readings {
		variance += (v - mean) * (v - mean)
	}
	stdDev := 0.0
	if len(s.Readings) > 0 {
		stdDev = math.Sqrt(variance / float64(len(s.Readings)))
	}

	// Calculate percentiles for latency
	latencyP95 := calculatePercentile(s.Latencies, 95)
	latencyP99 := calculatePercentile(s.Latencies, 99)
	var avgLatency time.Duration
	if len(s.Latencies) > 0 {
		var sum time.Duration
		for _, l := range s.Latencies {
			sum += l
		}
		avgLatency = sum / time.Duration(len(s.Latencies))
	}

	uptime := time.Since(s.StartTime)
	rate := float64(s.TotalReadings) / uptime.Seconds()

	log.Println("=== GLOBAL STATISTICS ===")
	log.Printf("Uptime: %v", uptime)
	log.Printf("Total Readings: %d", s.TotalReadings)
	log.Printf("Readings/sec: %.2f", rate)
	log.Printf("Mean: %.2f", mean)
	log.Printf("Std Dev: %.2f", stdDev)
	log.Printf("Min: %.2f", s.Min)
	log.Printf("Max: %.2f", s.Max)
	log.Printf("Active Edge Nodes: %d", len(s.EdgeNodes))
	log.Printf("Total Alerts: %d", len(s.Alerts))
	log.Printf("Latency - Avg: %v, P95: %v, P99: %v", avgLatency, latencyP95, latencyP99)
	
	// Edge node breakdown
	for edgeID, count := range s.EdgeNodes {
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

