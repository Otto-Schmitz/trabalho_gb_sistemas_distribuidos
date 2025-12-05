package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type DashboardData struct {
	mu              sync.RWMutex
	TotalReadings   int64            `json:"total_readings"`
	ReadingsPerSec  float64          `json:"readings_per_sec"`
	Mean            float64          `json:"mean"`
	StdDev          float64          `json:"std_dev"`
	Min             float64          `json:"min"`
	Max             float64          `json:"max"`
	ActiveEdgeNodes int              `json:"active_edge_nodes"`
	TotalAlerts     int              `json:"total_alerts"`
	AlertsByType    map[string]int   `json:"alerts_by_type"`
	AvgLatency      string           `json:"avg_latency"`
	LatencyP95      string           `json:"latency_p95"`
	LatencyP99      string           `json:"latency_p99"`
	Uptime          time.Duration    `json:"uptime"`
	RecentReadings  []ReadingDisplay `json:"recent_readings"`
	RecentAlerts    []AlertDisplay   `json:"recent_alerts"`
	LatencyHistory  []float64        `json:"latency_history"` // Last 60 seconds of avg latency in ms
	EdgeNodes       map[string]int   `json:"edge_nodes"`
	startTime       time.Time
	latencies       []time.Duration
	readings        []float64
	maxReadings     int
	maxAlerts       int
}

type ReadingDisplay struct {
	SensorID  string    `json:"sensor_id"`
	Value     float64   `json:"value"`
	EdgeID    string    `json:"edge_id"`
	Timestamp time.Time `json:"timestamp"`
}

type AlertDisplay struct {
	SensorID  string    `json:"sensor_id"`
	Value     float64   `json:"value"`
	EdgeID    string    `json:"edge_id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
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

func main() {
	var (
		natsURL     = flag.String("nats", "nats://localhost:4222", "NATS server URL")
		port        = flag.String("port", "8080", "Dashboard server port")
		maxReadings = flag.Int("max-readings", 1000, "Maximum readings to keep in memory")
		maxAlerts   = flag.Int("max-alerts", 100, "Maximum alerts to keep in memory")
	)
	flag.Parse()

	// Connect to NATS
	nc, err := nats.Connect(*natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	dashboard := &DashboardData{
		startTime:      time.Now(),
		EdgeNodes:      make(map[string]int),
		RecentReadings: make([]ReadingDisplay, 0),
		RecentAlerts:   make([]AlertDisplay, 0),
		AlertsByType:   make(map[string]int),
		latencies:      make([]time.Duration, 0),
		LatencyHistory: make([]float64, 0),
		readings:       make([]float64, 0),
		maxReadings:    *maxReadings,
		maxAlerts:      *maxAlerts,
		Min:            -1,
		Max:            -1,
	}

	// Subscribe to filtered readings
	_, err = nc.Subscribe("edge.filtered", func(msg *nats.Msg) {
		var filtered FilteredReading
		if err := json.Unmarshal(msg.Data, &filtered); err != nil {
			return
		}
		dashboard.processReading(filtered)
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
		dashboard.processAlert(alert)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to edge.alerts: %v", err)
	}

	// Setup HTTP routes
	http.HandleFunc("/", dashboard.handleIndex)
	http.HandleFunc("/api/data", dashboard.handleAPI)
	http.HandleFunc("/api/events", dashboard.handleSSE)

	log.Printf("Dashboard server starting on port %s", *port)
	log.Printf("Open http://localhost:%s in your browser", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

func (d *DashboardData) processReading(reading FilteredReading) {
	now := time.Now()
	latency := time.Duration(now.UnixMilli()-reading.Timestamp) * time.Millisecond
	if latency < 0 {
		latency = 0
	} // Prevent negative latency

	d.mu.Lock()
	defer d.mu.Unlock()

	d.TotalReadings++
	d.readings = append(d.readings, reading.Value)
	if len(d.readings) > d.maxReadings {
		d.readings = d.readings[1:]
	}

	// Update min/max
	if d.Min < 0 || reading.Value < d.Min {
		d.Min = reading.Value
	}
	if d.Max < 0 || reading.Value > d.Max {
		d.Max = reading.Value
	}

	// Track edge nodes
	d.EdgeNodes[reading.EdgeID]++
	d.ActiveEdgeNodes = len(d.EdgeNodes)

	// Track latencies
	d.latencies = append(d.latencies, latency)
	if len(d.latencies) > 1000 {
		d.latencies = d.latencies[1:]
	}

	// Add to recent readings
	display := ReadingDisplay{
		SensorID:  reading.SensorID,
		Value:     reading.Value,
		EdgeID:    reading.EdgeID,
		Timestamp: now,
	}
	d.RecentReadings = append([]ReadingDisplay{display}, d.RecentReadings...)
	if len(d.RecentReadings) > d.maxReadings {
		d.RecentReadings = d.RecentReadings[:d.maxReadings]
	}
}

func (d *DashboardData) processAlert(alert Alert) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.TotalAlerts++
	d.AlertsByType[alert.Type]++

	display := AlertDisplay{
		SensorID:  alert.SensorID,
		Value:     alert.Value,
		EdgeID:    alert.EdgeID,
		Type:      alert.Type,
		Message:   alert.Message,
		Timestamp: time.Now(),
	}

	d.RecentAlerts = append([]AlertDisplay{display}, d.RecentAlerts...)
	if len(d.RecentAlerts) > d.maxAlerts {
		d.RecentAlerts = d.RecentAlerts[:d.maxAlerts]
	}
}

func (d *DashboardData) getStats() DashboardData {
	d.mu.Lock() // Use Lock instead of RLock to update LatencyHistory safely
	defer d.mu.Unlock()

	stats := *d // Shallow copy
	// Manually copy maps and slices to avoid race conditions on read
	stats.EdgeNodes = make(map[string]int)
	for k, v := range d.EdgeNodes {
		stats.EdgeNodes[k] = v
	}
	stats.AlertsByType = make(map[string]int)
	for k, v := range d.AlertsByType {
		stats.AlertsByType[k] = v
	}

	stats.Uptime = time.Since(d.startTime)
	stats.ReadingsPerSec = float64(d.TotalReadings) / stats.Uptime.Seconds()

	// Calculate mean and std dev
	if len(d.readings) > 0 {
		var sum float64
		for _, v := range d.readings {
			sum += v
		}
		stats.Mean = sum / float64(len(d.readings))

		var variance float64
		for _, v := range d.readings {
			variance += (v - stats.Mean) * (v - stats.Mean)
		}
		if len(d.readings) > 1 {
			variance = variance / float64(len(d.readings))
			stats.StdDev = math.Sqrt(variance)
		}
	}

	// Calculate latency percentiles and history
	if len(d.latencies) > 0 {
		var sum time.Duration
		for _, l := range d.latencies {
			sum += l
		}
		avgLatency := sum / time.Duration(len(d.latencies))
		stats.AvgLatency = avgLatency.String()

		// Update history (keep last 60 points)
		d.LatencyHistory = append(d.LatencyHistory, float64(avgLatency.Milliseconds()))
		if len(d.LatencyHistory) > 60 {
			d.LatencyHistory = d.LatencyHistory[1:]
		}
		stats.LatencyHistory = make([]float64, len(d.LatencyHistory))
		copy(stats.LatencyHistory, d.LatencyHistory)

		// Simple percentile calculation
		sorted := make([]time.Duration, len(d.latencies))
		copy(sorted, d.latencies)
		// Bubble sort is slow but OK for 1000 items, better use sort.Slice in prod but avoiding import sort for minimal changes
		for i := 0; i < len(sorted)-1; i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[i] > sorted[j] {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}

		p95Idx := int(float64(len(sorted)) * 0.95)
		p99Idx := int(float64(len(sorted)) * 0.99)
		if p95Idx >= len(sorted) {
			p95Idx = len(sorted) - 1
		}
		if p99Idx >= len(sorted) {
			p99Idx = len(sorted) - 1
		}
		stats.LatencyP95 = sorted[p95Idx].String()
		stats.LatencyP99 = sorted[p99Idx].String()
	}

	return stats
}

func (d *DashboardData) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl := `<!DOCTYPE html>
<html lang="pt-BR">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sistema Distribuído - Dashboard</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --primary: #6366f1;
            --primary-dark: #4f46e5;
            --bg: #f3f4f6;
            --card-bg: #ffffff;
            --text: #1f2937;
            --text-light: #6b7280;
            --success: #10b981;
            --warning: #f59e0b;
            --danger: #ef4444;
        }
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: 'Inter', sans-serif;
            background-color: var(--bg);
            color: var(--text);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 1600px;
            margin: 0 auto;
        }
        .header {
            background: var(--card-bg);
            padding: 24px;
            border-radius: 16px;
            box-shadow: 0 4px 6px -1px rgba(0,0,0,0.1);
            margin-bottom: 24px;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .header h1 {
            color: var(--text);
            font-size: 1.5rem;
            display: flex;
            align-items: center;
            gap: 10px;
        }
        .status-badge {
            display: inline-flex;
            align-items: center;
            padding: 6px 12px;
            border-radius: 20px;
            font-size: 0.875rem;
            font-weight: 600;
            background: #fee2e2;
            color: var(--danger);
        }
        .status-badge.online {
            background: #d1fae5;
            color: var(--success);
        }
        .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            background: currentColor;
            margin-right: 8px;
        }
        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 24px;
            margin-bottom: 24px;
        }
        .card {
            background: var(--card-bg);
            padding: 24px;
            border-radius: 16px;
            box-shadow: 0 4px 6px -1px rgba(0,0,0,0.05);
            transition: transform 0.2s;
        }
        .card:hover {
            transform: translateY(-2px);
        }
        .card h2 {
            color: var(--text-light);
            font-size: 0.875rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 20px;
            font-weight: 600;
        }
        .metric-large {
            font-size: 2.5rem;
            font-weight: 700;
            color: var(--primary);
            margin-bottom: 8px;
        }
        .metric-row {
            display: flex;
            justify-content: space-between;
            margin-bottom: 12px;
            padding-bottom: 12px;
            border-bottom: 1px solid #f3f4f6;
        }
        .metric-row:last-child {
            border-bottom: none;
            margin-bottom: 0;
            padding-bottom: 0;
        }
        .chart-row {
            display: grid;
            grid-template-columns: 2fr 1fr;
            gap: 24px;
            margin-bottom: 24px;
            height: 400px;
        }
        .chart-card {
            background: var(--card-bg);
            padding: 24px;
            border-radius: 16px;
            box-shadow: 0 4px 6px -1px rgba(0,0,0,0.05);
            position: relative;
            height: 100%;
            display: flex;
            flex-direction: column;
        }
        .chart-wrapper {
            flex: 1;
            position: relative;
            min-height: 0;
        }
        table {
            width: 100%;
            border-collapse: separate;
            border-spacing: 0;
        }
        th {
            background: #f9fafb;
            color: var(--text-light);
            font-weight: 600;
            text-align: left;
            padding: 12px 16px;
            font-size: 0.75rem;
            text-transform: uppercase;
            position: sticky;
            top: 0;
            z-index: 10;
        }
        td {
            padding: 12px 16px;
            border-bottom: 1px solid #f3f4f6;
            font-size: 0.875rem;
        }
        tr:last-child td {
            border-bottom: none;
        }
        .badge {
            padding: 4px 10px;
            border-radius: 12px;
            font-size: 0.75rem;
            font-weight: 600;
        }
        .badge-threshold {
            background: #fee2e2;
            color: var(--danger);
        }
        .badge-info {
            background: #dbeafe;
            color: var(--primary);
        }
        .table-container {
            overflow-y: auto;
            max-height: 400px;
        }
        
        @media (max-width: 1024px) {
            .chart-row {
                grid-template-columns: 1fr;
                height: auto;
            }
            .chart-card {
                height: 400px;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1>📊 Sistema Distribuído</h1>
                <div style="color: var(--text-light); font-size: 0.875rem; margin-top: 4px;">Monitoramento em Tempo Real</div>
            </div>
            <div style="text-align: right;">
                <div class="status-badge online" id="status">
                    <span class="status-dot"></span>Online
                </div>
                <div id="uptime" style="margin-top: 8px; font-size: 0.875rem; color: var(--text-light);">
                    Uptime: 00h 00m 00s
                </div>
            </div>
        </div>

        <div class="grid">
            <div class="card">
                <h2>Fluxo de Dados</h2>
                <div class="metric-large" id="readings-per-sec">0.00</div>
                <div style="color: var(--text-light);">Leituras / segundo</div>
                <div style="margin-top: 16px; font-size: 0.875rem;">
                    Total: <strong id="total-readings">0</strong>
                </div>
            </div>

            <div class="card">
                <h2>Estatísticas (Valores)</h2>
                <div class="metric-row">
                    <span style="color: var(--text-light);">Média</span>
                    <strong id="mean">0.00</strong>
                </div>
                <div class="metric-row">
                    <span style="color: var(--text-light);">Min / Max</span>
                    <strong><span id="min">0</span> / <span id="max">0</span></strong>
                </div>
                <div class="metric-row">
                    <span style="color: var(--text-light);">Desvio Padrão</span>
                    <strong id="std-dev">0.00</strong>
                </div>
            </div>

            <div class="card">
                <h2>Saúde do Sistema</h2>
                <div class="metric-row">
                    <span style="color: var(--text-light);">Latência Média</span>
                    <strong id="avg-latency">0ms</strong>
                </div>
                <div class="metric-row">
                    <span style="color: var(--text-light);">P95 / P99</span>
                    <strong><span id="latency-p95">0ms</span> / <span id="latency-p99">0ms</span></strong>
                </div>
                <div class="metric-row">
                    <span style="color: var(--text-light);">Edge Nodes</span>
                    <strong id="active-edges" style="color: var(--success);">0 Ativos</strong>
                </div>
            </div>

            <div class="card">
                <h2>Alertas</h2>
                <div class="metric-large" id="total-alerts" style="color: var(--danger);">0</div>
                <div style="color: var(--text-light);">Total de Incidentes</div>
            </div>
        </div>

        <div class="chart-row">
            <div class="chart-card">
                <h2>Leituras & Latência</h2>
                <div class="chart-wrapper">
                    <canvas id="mainChart"></canvas>
                </div>
            </div>
            <div class="chart-card">
                <h2>Distribuição de Alertas</h2>
                <div class="chart-wrapper">
                    <canvas id="alertsChart"></canvas>
                </div>
            </div>
        </div>

        <div class="grid" style="grid-template-columns: 1fr 1fr;">
            <div class="card table-container">
                <h2>📋 Últimas Leituras</h2>
                <table id="readings-table">
                    <thead>
                        <tr>
                            <th>Sensor</th>
                            <th>Valor</th>
                            <th>Edge Node</th>
                            <th>Hora</th>
                        </tr>
                    </thead>
                    <tbody id="readings-tbody"></tbody>
                </table>
            </div>

            <div class="card table-container">
                <h2>🚨 Registro de Alertas</h2>
                <table id="alerts-table">
                    <thead>
                        <tr>
                            <th>Sensor</th>
                            <th>Valor</th>
                            <th>Tipo</th>
                            <th>Mensagem</th>
                            <th>Hora</th>
                        </tr>
                    </thead>
                    <tbody id="alerts-tbody"></tbody>
                </table>
            </div>
        </div>
    </div>

    <script>
        // Charts Configuration
        Chart.defaults.font.family = "'Inter', sans-serif";
        Chart.defaults.color = '#6b7280';
        
        let mainChart = null;
        let alertsChart = null;

        function initCharts() {
            // Main Chart (Readings & Latency)
            const ctxMain = document.getElementById('mainChart').getContext('2d');
            mainChart = new Chart(ctxMain, {
                type: 'line',
                data: {
                    labels: Array(60).fill(''),
                    datasets: [
                        {
                            label: 'Valor do Sensor',
                            data: Array(60).fill(null),
                            borderColor: '#6366f1',
                            backgroundColor: 'rgba(99, 102, 241, 0.1)',
                            borderWidth: 2,
                            tension: 0.4,
                            fill: true,
                            yAxisID: 'y'
                        },
                        {
                            label: 'Latência (ms)',
                            data: Array(60).fill(null),
                            borderColor: '#f59e0b',
                            borderWidth: 2,
                            borderDash: [5, 5],
                            tension: 0.4,
                            pointRadius: 0,
                            yAxisID: 'y1'
                        }
                    ]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    interaction: { mode: 'index', intersect: false },
                    plugins: {
                        legend: { position: 'top' }
                    },
                    scales: {
                        y: {
                            type: 'linear',
                            display: true,
                            position: 'left',
                            grid: { color: '#f3f4f6' }
                        },
                        y1: {
                            type: 'linear',
                            display: true,
                            position: 'right',
                            grid: { drawOnChartArea: false }
                        },
                        x: { grid: { display: false } }
                    }
                }
            });

            // Alerts Chart (Doughnut)
            const ctxAlerts = document.getElementById('alertsChart').getContext('2d');
            alertsChart = new Chart(ctxAlerts, {
                type: 'doughnut',
                data: {
                    labels: ['Threshold', 'Anomaly', 'Other'],
                    datasets: [{
                        data: [0, 0, 0],
                        backgroundColor: ['#ef4444', '#f59e0b', '#6b7280'],
                        borderWidth: 0
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: {
                        legend: { position: 'bottom' }
                    },
                    cutout: '70%'
                }
            });
        }

        function updateDashboard(data) {
            // Metrics
            document.getElementById('total-readings').innerText = data.total_readings.toLocaleString();
            document.getElementById('readings-per-sec').innerText = data.readings_per_sec.toFixed(1);
            document.getElementById('mean').innerText = data.mean.toFixed(2);
            document.getElementById('min').innerText = data.min.toFixed(1);
            document.getElementById('max').innerText = data.max.toFixed(1);
            document.getElementById('std-dev').innerText = data.std_dev.toFixed(2);
            document.getElementById('avg-latency').innerText = data.avg_latency || '0ms';
            document.getElementById('latency-p95').innerText = data.latency_p95 || '0ms';
            document.getElementById('latency-p99').innerText = data.latency_p99 || '0ms';
            document.getElementById('active-edges').innerText = data.active_edge_nodes + ' Ativos';
            document.getElementById('total-alerts').innerText = data.total_alerts;

            // Uptime
            const hours = Math.floor(data.uptime / 3600).toString().padStart(2, '0');
            const minutes = Math.floor((data.uptime % 3600) / 60).toString().padStart(2, '0');
            const seconds = Math.floor(data.uptime % 60).toString().padStart(2, '0');
            document.getElementById('uptime').innerText = 'Uptime: ' + hours + 'h ' + minutes + 'm ' + seconds + 's';

            // Update Main Chart
            if (data.recent_readings) {
                const readings = data.recent_readings.slice(0, 60).reverse();
                const values = readings.map(r => r.value);
                
                // Update readings dataset
                mainChart.data.datasets[0].data = values;
                
                // Update latency dataset if history available
                if (data.latency_history) {
                     mainChart.data.datasets[1].data = data.latency_history;
                }
                
                mainChart.update('none');
            }

            // Update Alerts Chart
            if (data.alerts_by_type) {
                const types = Object.keys(data.alerts_by_type);
                const counts = Object.values(data.alerts_by_type);
                
                if (types.length > 0) {
                    alertsChart.data.labels = types;
                    alertsChart.data.datasets[0].data = counts;
                    alertsChart.update();
                }
            }

            // Update Readings Table
            const readingsBody = document.getElementById('readings-tbody');
            readingsBody.innerHTML = data.recent_readings.slice(0, 15).map(function(r) {
                return '<tr>' +
                    '<td style="font-family: monospace;">' + r.sensor_id + '</td>' +
                    '<td>' + r.value.toFixed(2) + '</td>' +
                    '<td style="font-size: 0.75rem; color: #6b7280;">' + r.edge_id + '</td>' +
                    '<td>' + new Date(r.timestamp).toLocaleTimeString() + '</td>' +
                '</tr>';
            }).join('');

            // Update Alerts Table
            const alertsBody = document.getElementById('alerts-tbody');
            alertsBody.innerHTML = data.recent_alerts.slice(0, 15).map(function(a) {
                return '<tr>' +
                    '<td style="font-family: monospace;">' + a.sensor_id + '</td>' +
                    '<td>' + a.value.toFixed(2) + '</td>' +
                    '<td><span class="badge badge-threshold">' + a.type + '</span></td>' +
                    '<td style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">' + a.message + '</td>' +
                    '<td>' + new Date(a.timestamp).toLocaleTimeString() + '</td>' +
                '</tr>';
            }).join('');
        }

        function connectSSE() {
            const evtSource = new EventSource("/api/events");
            const statusBadge = document.getElementById('status');
            
            evtSource.onmessage = (event) => {
                const data = JSON.parse(event.data);
                updateDashboard(data);
                
                if (!statusBadge.classList.contains('online')) {
                    statusBadge.className = 'status-badge online';
                    statusBadge.innerHTML = '<span class="status-dot"></span>Online';
                }
            };

            evtSource.onerror = (err) => {
                statusBadge.className = 'status-badge';
                statusBadge.style.background = '#fee2e2';
                statusBadge.style.color = '#ef4444';
                statusBadge.innerHTML = '<span class="status-dot"></span>Reconnecting...';
                evtSource.close();
                setTimeout(connectSSE, 3000);
            };
        }

        document.addEventListener('DOMContentLoaded', () => {
            initCharts();
            connectSSE();
        });
    </script>
</body>
</html>`

	t, _ := template.New("dashboard").Parse(tmpl)
	t.Execute(w, nil)
}

func (d *DashboardData) handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	stats := d.getStats()
	json.NewEncoder(w).Encode(stats)
}

func (d *DashboardData) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := d.getStats()
			data, _ := json.Marshal(stats)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}
