package Services

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	healthLogDir    = "logs/health"
	healthInterval  = 30 * time.Second
	healthRetention = 30 * 24 * time.Hour
	healthCSVHeader = "timestamp,ram_total,ram_used,ram_available,cpu_pct,http_requests,http_latency_buckets,ws_active\n"
)

var (
	prevCPUSample       *cpuSample
	prevCaddySample     *caddySample
	wsConnectionCounter func() int
	caddyMetricsURL     string
)

// --- Subscriber / broadcaster plumbing ---

type HealthUpdate struct {
	RAM  map[string]HealthRAMEntry  `json:"ram"`
	CPU  map[string]HealthCPUEntry  `json:"cpu"`
	HTTP map[string]HealthHTTPEntry `json:"http"`
	WS   map[string]HealthWSEntry   `json:"ws"`
}

var (
	healthSubscribers   = map[int]bool{}
	healthSubscribersMu sync.Mutex
	healthBroadcaster   func(HealthUpdate)
)

func SetHealthBroadcaster(fn func(HealthUpdate)) {
	healthBroadcaster = fn
}

func SetWSConnectionCounter(fn func() int) {
	wsConnectionCounter = fn
}

func SubscribeHealth(userID int) {
	healthSubscribersMu.Lock()
	healthSubscribers[userID] = true
	healthSubscribersMu.Unlock()
}

func UnsubscribeHealth(userID int) {
	healthSubscribersMu.Lock()
	delete(healthSubscribers, userID)
	healthSubscribersMu.Unlock()
}

func GetHealthSubscribers() []int {
	healthSubscribersMu.Lock()
	defer healthSubscribersMu.Unlock()
	ids := make([]int, 0, len(healthSubscribers))
	for id := range healthSubscribers {
		ids = append(ids, id)
	}
	return ids
}

// --- Init ---

func InitHealthMonitor() {
	caddyMetricsURL = os.Getenv("CADDY_METRICS_URL")
	if caddyMetricsURL == "" {
		caddyMetricsURL = "http://localhost:2019/metrics"
	}

	if err := os.MkdirAll(healthLogDir, 0755); err != nil {
		log.Printf("Health monitor: failed to create log directory: %v", err)
		return
	}

	go func() {
		ticker := time.NewTicker(healthInterval)
		defer ticker.Stop()
		runHealthCheck()
		for range ticker.C {
			runHealthCheck()
		}
	}()
}

// --- RAM ---

type systemMemStats struct {
	Total     uint64
	Available uint64
	Used      uint64
}

func readSystemMem() (systemMemStats, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return systemMemStats{}, err
	}
	defer f.Close()

	fields := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		fields[key] = val * 1024 // /proc/meminfo values are in kB
	}

	total := fields["MemTotal"]
	available := fields["MemAvailable"]
	return systemMemStats{
		Total:     total,
		Available: available,
		Used:      total - available,
	}, nil
}

// --- CPU ---

type cpuSample struct {
	total uint64
	idle  uint64
}

func readCPUSample() (*cpuSample, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		// cpu user nice system idle iowait irq softirq steal guest guest_nice
		fields := strings.Fields(line)[1:]
		var vals [10]uint64
		for i := 0; i < len(fields) && i < 10; i++ {
			vals[i], _ = strconv.ParseUint(fields[i], 10, 64)
		}
		idle := vals[3] + vals[4] // idle + iowait
		total := vals[0] + vals[1] + vals[2] + vals[3] + vals[4] +
			vals[5] + vals[6] + vals[7] + vals[8] + vals[9]
		return &cpuSample{total: total, idle: idle}, nil
	}
	return nil, fmt.Errorf("cpu line not found in /proc/stat")
}

// --- Caddy metrics ---

type caddySample struct {
	requests float64
	buckets  map[string]float64 // le -> cumulative count
}

func scrapeCaddy() (*caddySample, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(caddyMetricsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	sample := &caddySample{buckets: map[string]float64{}}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "caddy_http_requests_total{") {
			sample.requests += prometheusValue(line)
		} else if strings.HasPrefix(line, "caddy_http_request_duration_seconds_bucket{") {
			le := prometheusLabel(line, "le")
			if le != "" {
				sample.buckets[le] += prometheusValue(line)
			}
		}
	}
	return sample, nil
}

func prometheusValue(line string) float64 {
	var valueStr string
	if idx := strings.Index(line, "} "); idx != -1 {
		valueStr = strings.Fields(line[idx+2:])[0]
	} else {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			valueStr = parts[1]
		}
	}
	v, _ := strconv.ParseFloat(valueStr, 64)
	return v
}

func prometheusLabel(line, key string) string {
	search := key + `="`
	idx := strings.Index(line, search)
	if idx == -1 {
		return ""
	}
	start := idx + len(search)
	end := strings.Index(line[start:], `"`)
	if end == -1 {
		return ""
	}
	return line[start : start+end]
}

func encodeBuckets(buckets map[string]float64) string {
	if len(buckets) == 0 {
		return ""
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return leToFloat(keys[i]) < leToFloat(keys[j])
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%.0f", k, buckets[k]))
	}
	return strings.Join(parts, "|")
}

func decodeBuckets(s string) map[string]float64 {
	result := map[string]float64{}
	if s == "" {
		return result
	}
	for _, pair := range strings.Split(s, "|") {
		kv := strings.SplitN(pair, ":", 2)
		if len(kv) == 2 {
			if v, err := strconv.ParseFloat(kv[1], 64); err == nil {
				result[kv[0]] = v
			}
		}
	}
	return result
}

func leToFloat(le string) float64 {
	if le == "+Inf" {
		return math.Inf(1)
	}
	v, _ := strconv.ParseFloat(le, 64)
	return v
}

// --- Main check ---

func runHealthCheck() {
	now := time.Now()
	ts := now.Format("2006-01-02 15:04:05")

	mem, memErr := readSystemMem()
	cur, cpuErr := readCPUSample()
	caddyCur, _ := scrapeCaddy()

	// RAM
	var ramTotal, ramUsed, ramAvailable string
	if memErr == nil {
		ramTotal = strconv.FormatUint(mem.Total, 10)
		ramUsed = strconv.FormatUint(mem.Used, 10)
		ramAvailable = strconv.FormatUint(mem.Available, 10)
	}

	// CPU
	var cpuPct string
	if cpuErr == nil && prevCPUSample != nil {
		deltaTotal := cur.total - prevCPUSample.total
		deltaIdle := cur.idle - prevCPUSample.idle
		if deltaTotal > 0 {
			cpuPct = fmt.Sprintf("%.2f", float64(deltaTotal-deltaIdle)/float64(deltaTotal)*100)
		}
	}
	if cur != nil {
		prevCPUSample = cur
	}

	// HTTP (Caddy delta)
	var httpRequests, httpBuckets string
	if caddyCur != nil && prevCaddySample != nil {
		delta := int64(caddyCur.requests - prevCaddySample.requests)
		if delta < 0 {
			delta = 0 // Caddy restarted
		}
		httpRequests = strconv.FormatInt(delta, 10)

		deltaBuckets := map[string]float64{}
		for le, count := range caddyCur.buckets {
			prev := prevCaddySample.buckets[le]
			d := count - prev
			if d < 0 {
				d = 0
			}
			deltaBuckets[le] = d
		}
		httpBuckets = encodeBuckets(deltaBuckets)
	}
	if caddyCur != nil {
		prevCaddySample = caddyCur
	}

	// WS active connections
	var wsActive string
	if wsConnectionCounter != nil {
		wsActive = strconv.Itoa(wsConnectionCounter())
	}

	row := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s\n",
		ts,
		ramTotal, ramUsed, ramAvailable,
		cpuPct,
		httpRequests, httpBuckets,
		wsActive,
	)

	csvPath := filepath.Join(healthLogDir, fmt.Sprintf("health_%s.csv", now.Format("2006-01-02")))
	isNew := false
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		isNew = true
	}

	f, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Health monitor: failed to open CSV file: %v", err)
		return
	}
	defer f.Close()

	if isNew {
		if _, err := f.WriteString(healthCSVHeader); err != nil {
			log.Printf("Health monitor: failed to write CSV header: %v", err)
			return
		}
	}
	if _, err := f.WriteString(row); err != nil {
		log.Printf("Health monitor: failed to write CSV row: %v", err)
	}

	if healthBroadcaster != nil {
		update := HealthUpdate{
			RAM:  map[string]HealthRAMEntry{},
			CPU:  map[string]HealthCPUEntry{},
			HTTP: map[string]HealthHTTPEntry{},
			WS:   map[string]HealthWSEntry{},
		}
		if memErr == nil {
			update.RAM[ts] = HealthRAMEntry{Total: mem.Total, Used: mem.Used, Available: mem.Available}
		}
		if cpuPct != "" {
			if pct, err := strconv.ParseFloat(cpuPct, 64); err == nil {
				update.CPU[ts] = HealthCPUEntry{Pct: pct}
			}
		}
		if httpRequests != "" {
			reqs, _ := strconv.ParseInt(httpRequests, 10, 64)
			update.HTTP[ts] = HealthHTTPEntry{Requests: reqs, LatencyBuckets: decodeBuckets(httpBuckets)}
		}
		if wsActive != "" {
			active, _ := strconv.Atoi(wsActive)
			update.WS[ts] = HealthWSEntry{Active: active}
		}
		go healthBroadcaster(update)
	}

	pruneOldHealthLogs()
}

// --- Response types ---

type HealthRAMEntry struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}

type HealthCPUEntry struct {
	Pct float64 `json:"pct"`
}

type HealthHTTPEntry struct {
	Requests       int64              `json:"requests"`
	LatencyBuckets map[string]float64 `json:"latency_buckets"`
}

type HealthWSEntry struct {
	Active int `json:"active"`
}

type HealthData struct {
	RAM  map[string]HealthRAMEntry  `json:"ram"`
	CPU  map[string]HealthCPUEntry  `json:"cpu"`
	HTTP map[string]HealthHTTPEntry `json:"http"`
	WS   map[string]HealthWSEntry   `json:"ws"`
}

// ReadHealthData reads CSV files covering [start, end] and returns parsed entries.
func ReadHealthData(start, end time.Time) HealthData {
	result := HealthData{
		RAM:  map[string]HealthRAMEntry{},
		CPU:  map[string]HealthCPUEntry{},
		HTTP: map[string]HealthHTTPEntry{},
		WS:   map[string]HealthWSEntry{},
	}

	for day := start.Truncate(24 * time.Hour); !day.After(end.Truncate(24 * time.Hour)); day = day.Add(24 * time.Hour) {
		csvPath := filepath.Join(healthLogDir, fmt.Sprintf("health_%s.csv", day.Format("2006-01-02")))
		f, err := os.Open(csvPath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		scanner.Scan() // skip header
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), ",")
			if len(fields) < 8 {
				continue
			}
			ts, err := time.ParseInLocation("2006-01-02 15:04:05", fields[0], time.Local)
			if err != nil || ts.Before(start) || ts.After(end) {
				continue
			}
			key := fields[0]

			ramTotal, e1 := strconv.ParseUint(fields[1], 10, 64)
			ramUsed, e2 := strconv.ParseUint(fields[2], 10, 64)
			ramAvailable, e3 := strconv.ParseUint(fields[3], 10, 64)
			if e1 == nil && e2 == nil && e3 == nil {
				result.RAM[key] = HealthRAMEntry{Total: ramTotal, Used: ramUsed, Available: ramAvailable}
			}

			if fields[4] != "" {
				if pct, err := strconv.ParseFloat(fields[4], 64); err == nil {
					result.CPU[key] = HealthCPUEntry{Pct: pct}
				}
			}

			if fields[5] != "" {
				reqs, err := strconv.ParseInt(fields[5], 10, 64)
				if err == nil {
					result.HTTP[key] = HealthHTTPEntry{
						Requests:       reqs,
						LatencyBuckets: decodeBuckets(fields[6]),
					}
				}
			}

			if fields[7] != "" {
				if active, err := strconv.Atoi(fields[7]); err == nil {
					result.WS[key] = HealthWSEntry{Active: active}
				}
			}
		}
		f.Close()
	}

	return result
}

// --- Cleanup ---

func pruneOldHealthLogs() {
	cutoff := time.Now().Add(-healthRetention)
	entries, err := os.ReadDir(healthLogDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(healthLogDir, entry.Name()))
		}
	}
}
