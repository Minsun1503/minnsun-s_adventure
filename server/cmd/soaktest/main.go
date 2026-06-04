package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ─── JSON Response Types ──────────────────────────────────────────────────────

type State struct {
	OnlinePlayers int    `json:"online_players"`
	TotalMonsters int    `json:"total_monsters"`
	GroundItems   int    `json:"ground_items"`
	EntityIDMax   uint64 `json:"entity_id_max"`
	RecycledIDs   int    `json:"recycled_ids"`
	GridEntities  int    `json:"grid_entities"`
	RunningMaps   []int  `json:"running_maps"`
}

type Perf struct {
	TickAvgNs   int64  `json:"tick_avg_ns"`
	TickMaxNs   int64  `json:"tick_max_ns"`
	TickCount   uint64 `json:"tick_count"`
	Goroutines  int    `json:"goroutines"`
	NumGC       uint32 `json:"num_gc"`
	LastPauseNs uint64 `json:"last_pause_ns"`
	AllocBytes  uint64 `json:"alloc_bytes"`
	HeapObjects uint64 `json:"heap_objects"`
}

type Ops struct {
	SaveQueueSize  int    `json:"save_queue_size"`
	SaveQueueCap   int    `json:"save_queue_cap"`
	HeapAllocMB    uint64 `json:"heap_alloc_mb"`
	TotalAllocMB   uint64 `json:"total_alloc_mb"`
	SysMemMB       uint64 `json:"sys_mem_mb"`
	NumGCCycles    uint32 `json:"num_gc_cycles"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	GoroutineDelta int    `json:"goroutine_delta"`
	PauseAvgNs     uint64 `json:"pause_avg_ns"`
}

// ─── Snapshot ─────────────────────────────────────────────────────────────────

type Snapshot struct {
	Timestamp   time.Time
	State       State
	Perf        Perf
	Ops         Ops
	TickAvgMs   float64
	HeapAllocMB uint64
	SavePct     float64
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	baseURL := "http://localhost:9090"
	if len(os.Args) > 1 {
		baseURL = os.Args[1]
	}

	duration := 24 * time.Hour
	if len(os.Args) > 2 {
		if parsed, err := time.ParseDuration(os.Args[2]); err == nil {
			duration = parsed
		}
	}

	interval := 5 * time.Second
	if len(os.Args) > 3 {
		if parsed, err := time.ParseDuration(os.Args[3]); err == nil {
			interval = parsed
		}
	}

	fmt.Printf("=== Soak Test: Long-Run Stability ===\n")
	fmt.Printf("Admin URL: %s\n", baseURL)
	fmt.Printf("Duration:  %v\n", duration)
	fmt.Printf("Interval:  %v\n", interval)
	fmt.Println()

	// Signal handling for early termination
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	client := &http.Client{Timeout: 5 * time.Second}
	startTime := time.Now()

	var snapshots []Snapshot
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Pre-compute trends
	// elapsed tracks how long the test has been running
	elapsed := time.Duration(0)
	var (
		prevAllocMB    uint64
		prevGoroutines int
		prevGC         uint32
	)

	fmt.Printf("%-25s | %6s | %6s | %8s | %6s | %6s | %6s | %6s\n",
		"Timestamp", "TickMs", "HeapMB", "Goroutines", "GC#", "GCd", "SQueue", "Save%%")
	fmt.Println("-" + "-----|" + "------|" + "------|" + "--------|" +
		"------|" + "------|" + "------|" + "-------")

	for {
		select {
		case <-sigCh:
			fmt.Println("\n\n=== Test interrupted, generating report ===")
			generateReport(snapshots, startTime, startTime)
			return

		case <-ticker.C:
			snap := fetchSnapshot(client, baseURL)
			if snap != nil {
				snapshots = append(snapshots, *snap)
				elapsed = time.Since(startTime)

				// Compute deltas
				savePct := float64(snap.Ops.SaveQueueSize) / float64(snap.Ops.SaveQueueCap) * 100
				gDelta := snap.Perf.Goroutines - prevGoroutines
				gcDelta := snap.Ops.NumGCCycles - prevGC
				allocGrowth := snap.HeapAllocMB - prevAllocMB

				fmt.Printf("%-25s | %6.2f | %5dMB | %5d(%+d) | %4d(+%d) | %+5d | %3d/%d | %5.1f%%\n",
					snap.Timestamp.Format("2006-01-02 15:04:05"),
					snap.TickAvgMs,
					snap.HeapAllocMB,
					snap.Perf.Goroutines, gDelta,
					snap.Ops.NumGCCycles, gcDelta,
					allocGrowth,
					snap.Ops.SaveQueueSize, snap.Ops.SaveQueueCap,
					savePct,
				)

				prevAllocMB = snap.HeapAllocMB
				prevGoroutines = snap.Perf.Goroutines
				prevGC = snap.Ops.NumGCCycles
			} else {
				fmt.Printf("%-25s | FAILED\n", time.Now().Format("2006-01-02 15:04:05"))
			}

			if elapsed >= duration {
				fmt.Println("\n\n=== Test duration reached, generating final report ===")
				generateReport(snapshots, startTime, startTime)
				return
			}
		}
	}
}

// fetchSnapshot collects all metrics from the admin API.
func fetchSnapshot(client *http.Client, baseURL string) *Snapshot {
	snap := &Snapshot{Timestamp: time.Now()}

	// State
	if resp, err := client.Get(baseURL + "/debug/state"); err == nil {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		json.Unmarshal(body, &snap.State)
	} else {
		return nil
	}

	// Perf
	if resp, err := client.Get(baseURL + "/debug/perf"); err == nil {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		json.Unmarshal(body, &snap.Perf)
	} else {
		return nil
	}

	// Ops
	if resp, err := client.Get(baseURL + "/debug/ops"); err == nil {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		json.Unmarshal(body, &snap.Ops)
	} else {
		return nil
	}

	snap.TickAvgMs = float64(snap.Perf.TickAvgNs) / 1_000_000.0
	snap.HeapAllocMB = snap.Perf.AllocBytes / 1_000_000
	if snap.Ops.SaveQueueCap > 0 {
		snap.SavePct = float64(snap.Ops.SaveQueueSize) / float64(snap.Ops.SaveQueueCap) * 100
	}

	return snap
}

// generateReport writes a summary report to stdout and a JSON file.
func generateReport(snapshots []Snapshot, start, end time.Time) {
	if len(snapshots) == 0 {
		fmt.Println("No data collected.")
		return
	}

	// Compute summary
	var (
		minTickMs    float64 = 9999
		maxTickMs    float64
		sumTickMs    float64
		minHeap      uint64 = 999999999
		maxHeap      uint64
		totalHeap    uint64
		maxGoroutine int
		minGoroutine int = 999999
		maxSavePct   float64
		firstGC      uint32
		lastGC       uint32
	)

	for _, s := range snapshots {
		if s.TickAvgMs < minTickMs {
			minTickMs = s.TickAvgMs
		}
		if s.TickAvgMs > maxTickMs {
			maxTickMs = s.TickAvgMs
		}
		sumTickMs += s.TickAvgMs

		if s.HeapAllocMB < minHeap {
			minHeap = s.HeapAllocMB
		}
		if s.HeapAllocMB > maxHeap {
			maxHeap = s.HeapAllocMB
		}
		totalHeap += s.HeapAllocMB

		if s.Perf.Goroutines > maxGoroutine {
			maxGoroutine = s.Perf.Goroutines
		}
		if s.Perf.Goroutines < minGoroutine {
			minGoroutine = s.Perf.Goroutines
		}

		if s.SavePct > maxSavePct {
			maxSavePct = s.SavePct
		}
	}

	if firstGC = snapshots[0].Perf.NumGC; firstGC > 0 {
		firstGC = snapshots[0].Perf.NumGC
	}
	lastGC = snapshots[len(snapshots)-1].Perf.NumGC
	gcTotal := lastGC - firstGC
	duration := time.Since(snapshots[0].Timestamp)
	hours := duration.Hours()
	gcPerHour := float64(gcTotal) / hours

	avgTickMs := sumTickMs / float64(len(snapshots))
	avgHeap := totalHeap / uint64(len(snapshots))

	// Deduce overview
	leakStatus := "GOOD"
	if maxHeap > minHeap*2 && hours > 1 {
		leakStatus = "WARNING: Heap grew >2x"
	}
	if maxGoroutine-minGoroutine > 50 && hours > 1 {
		leakStatus += " | GOROUTINE LEAK"
	}
	if maxSavePct > 80 {
		leakStatus += " | SAVE QUEUE BACKPRESSURE"
	}
	if maxTickMs > 50 {
		leakStatus += " | TICK EXCEEDS 50ms"
	}

	report := fmt.Sprintf(`
══════════════════════════════════════════════════
  Soak Test Report
══════════════════════════════════════════════════

  Duration:       %v
  Samples:        %d
  Interval:       %ds

── Tick Performance ──
  Avg:            %.3f ms
  Min:            %.3f ms
  Max:            %.3f ms

── Memory ──
  Avg Heap:       %d MB
  Min Heap:       %d MB
  Max Heap:       %d MB

── Goroutines ──
  Min:            %d
  Max:            %d
  Delta:          %+d

── GC ──
  Total GCs:      %d
  GCs/Hour:       %.1f
  Last Pause:     %.3f ms (avg)

── Queue ──
  Max Save %%:     %.1f%%

── Status ──
  %s

══════════════════════════════════════════════════
`, duration, len(snapshots), int(duration.Seconds()/float64(len(snapshots))),
		avgTickMs, minTickMs, maxTickMs,
		avgHeap, minHeap, maxHeap,
		minGoroutine, maxGoroutine, maxGoroutine-minGoroutine,
		gcTotal, gcPerHour,
		float64(snapshots[len(snapshots)-1].Perf.LastPauseNs)/1_000_000.0,
		maxSavePct,
		leakStatus)

	fmt.Print(report)

	// Write to JSON file
	filename := fmt.Sprintf("soak_report_%s.json",
		time.Now().Format("20060102_150405"))
	data, _ := json.MarshalIndent(map[string]interface{}{
		"duration":       duration.String(),
		"samples":        len(snapshots),
		"avg_tick_ms":    avgTickMs,
		"min_tick_ms":    minTickMs,
		"max_tick_ms":    maxTickMs,
		"avg_heap_mb":    avgHeap,
		"min_heap_mb":    minHeap,
		"max_heap_mb":    maxHeap,
		"min_goroutines": minGoroutine,
		"max_goroutines": maxGoroutine,
		"gc_total":       gcTotal,
		"gc_per_hour":    gcPerHour,
		"max_save_pct":   maxSavePct,
		"status":         leakStatus,
	}, "", "  ")

	if err := ioutil.WriteFile(filename, data, 0644); err != nil {
		fmt.Printf("Failed to write report file: %v\n", err)
	} else {
		fmt.Printf("Report saved to: %s\n", filename)
	}
}
