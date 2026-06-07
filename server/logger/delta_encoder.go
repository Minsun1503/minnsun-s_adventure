// Package logger — Delta + Anomaly Encoder
//
// DeltaEncoder tracks previous field values for a given source and computes
// human-readable delta strings with anomaly tags. Designed for client-side
// telemetry where computing per-frame differences is more useful than logging
// every absolute value.
package logger

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ─── DeltaEncoder ─────────────────────────────────────────────────────────────

// DeltaEncoder maintains previous state for one source (e.g. "client") and
// produces delta lines of the form:
//
//	t=<ms> <changed_fields> [TAG]
//
// Anomaly rules are applied heuristically. If nothing changed and no anomaly
// is detected, Encode returns an empty string.
type DeltaEncoder struct {
	prevHP         int
	prevMP         int
	prevFPS        float64
	prevPing       float64
	prevPosX       float64
	prevPosZ       float64
	prevConnStatus string
	stuckCount     int
	initialized    bool
}

// Encode reads values from fields, compares with the previous state, and
// returns a delta string. Returns "" when nothing of interest changed.
func (d *DeltaEncoder) Encode(fields map[string]any, trigger string) string {
	if fields == nil {
		return ""
	}

	var (
		nowHP         = int(toFloat64(fields["hp"]))
		nowMP         = int(toFloat64(fields["mp"]))
		nowFPS        = toFloat64(fields["fps"])
		nowPing       = toFloat64(fields["ping"])
		nowPosX       = toFloat64(fields["pos_x"])
		nowPosZ       = toFloat64(fields["pos_z"])
		nowConnStatus = toString(fields["conn_status"])
	)

	var parts []string
	var tags []string

	// ── First call → NEW ──────────────────────────────────────────────────────
	if !d.initialized {
		d.initialized = true
		tags = append(tags, "NEW")

		// Still emit initial values so the operator has a baseline.
		parts = append(parts, fmt.Sprintf("hp=%d", nowHP))
		parts = append(parts, fmt.Sprintf("mp=%d", nowMP))
		parts = append(parts, fmt.Sprintf("fps=%.1f", nowFPS))
		parts = append(parts, fmt.Sprintf("ping=%.0f", nowPing))
		parts = append(parts, fmt.Sprintf("pos=(%.2f,%.2f)", nowPosX, nowPosZ))
		if nowConnStatus != "" {
			parts = append(parts, fmt.Sprintf("conn=%s", nowConnStatus))
		}
	} else {
		// ── HP delta ──────────────────────────────────────────────────────────
		if nowHP != d.prevHP {
			hpDiff := nowHP - d.prevHP
			sign := ""
			if hpDiff > 0 {
				sign = "+"
			}
			parts = append(parts, fmt.Sprintf("hp=%d(%s%d)", nowHP, sign, hpDiff))

			// Anomaly: HP dropped more than 30% of previous.
			if d.prevHP > 0 && float64(d.prevHP-nowHP) > float64(d.prevHP)*0.3 {
				tags = append(tags, "ANOMALY:HP_DROP")
			}
		}

		// ── MP delta ──────────────────────────────────────────────────────────
		if nowMP != d.prevMP {
			mpDiff := nowMP - d.prevMP
			sign := ""
			if mpDiff > 0 {
				sign = "+"
			}
			parts = append(parts, fmt.Sprintf("mp=%d(%s%d)", nowMP, sign, mpDiff))
		}

		// ── FPS delta ─────────────────────────────────────────────────────────
		if nowFPS != d.prevFPS {
			fpsDiff := nowFPS - d.prevFPS
			sign := ""
			if fpsDiff > 0 {
				sign = "+"
			}
			parts = append(parts, fmt.Sprintf("fps=%.1f(%s%.1f)", nowFPS, sign, fpsDiff))
		}

		// Anomaly: FPS < 20.
		if nowFPS > 0 && nowFPS < 20 {
			tags = append(tags, "ANOMALY:FPS_LOW")
		}

		// ── Ping delta ────────────────────────────────────────────────────────
		if nowPing != d.prevPing {
			pingDiff := nowPing - d.prevPing
			sign := ""
			if pingDiff > 0 {
				sign = "+"
			}
			parts = append(parts, fmt.Sprintf("ping=%.0f(%s%.0f)", nowPing, sign, pingDiff))
		}

		// ── Position delta ────────────────────────────────────────────────────
		posChanged := nowPosX != d.prevPosX || nowPosZ != d.prevPosZ
		if posChanged {
			parts = append(parts, fmt.Sprintf("pos=(%.2f,%.2f)", nowPosX, nowPosZ))
			d.stuckCount = 0
		} else {
			d.stuckCount++
		}

		// Anomaly: stuck (position unchanged for 10 consecutive calls).
		if d.stuckCount >= 10 {
			tags = append(tags, "ANOMALY:STUCK")
		}

		// ── Connection status delta ───────────────────────────────────────────
		if nowConnStatus != d.prevConnStatus && nowConnStatus != "" {
			parts = append(parts, fmt.Sprintf("conn=%s", nowConnStatus))
			tags = append(tags, "ANOMALY:CONN")
		}
	}

	// ── Trigger-based anomaly ─────────────────────────────────────────────────
	if trigger == "error" {
		tags = append(tags, "ANOMALY:ERROR")
	}

	// ── Persist current state ─────────────────────────────────────────────────
	d.prevHP = nowHP
	d.prevMP = nowMP
	d.prevFPS = nowFPS
	d.prevPing = nowPing
	d.prevPosX = nowPosX
	d.prevPosZ = nowPosZ
	d.prevConnStatus = nowConnStatus

	// ── Build output ─────────────────────────────────────────────────────────
	if len(parts) == 0 && len(tags) == 0 {
		return ""
	}

	now := time.Now().UnixMilli()
	tagStr := ""
	if len(tags) > 0 {
		tagStr = " [" + strings.Join(tags, "][") + "]"
	}

	return fmt.Sprintf("t=%d src=client %s%s", now, strings.Join(parts, " "), tagStr)
}

// ─── Global Encoder Registry ──────────────────────────────────────────────────

var (
	deltaEncoders  = make(map[string]*DeltaEncoder)
	deltaEncoderMu sync.Mutex
)

// GetDeltaEncoder returns the DeltaEncoder for the given source, creating one
// if it does not yet exist. Safe for concurrent use.
func GetDeltaEncoder(source string) *DeltaEncoder {
	deltaEncoderMu.Lock()
	defer deltaEncoderMu.Unlock()

	enc, ok := deltaEncoders[source]
	if !ok {
		enc = &DeltaEncoder{}
		deltaEncoders[source] = enc
	}
	return enc
}

// ─── Internal Helpers ─────────────────────────────────────────────────────────

func toFloat64(v any) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}
