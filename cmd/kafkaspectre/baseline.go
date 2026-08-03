package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
	"github.com/spf13/cobra"
)

// WO-48: baseline snapshot format. Stores per-topic status and lag so
// subsequent runs can compute deltas. The file is a simple JSON snapshot
// the operator manages (git, S3, etc.).
type TopicSnapshot struct {
	Name   string `json:"name"`
	Status string `json:"status"` // active, stale, unused, managed
	Lag    int64  `json:"lag"`
}

type BaselineSnapshot struct {
	Version   string          `json:"version"`
	Timestamp string          `json:"timestamp"`
	Topics    []TopicSnapshot `json:"topics"`
}

// WO-48: compute a snapshot from an audit result for comparison.
func snapshotFromResult(result *reporter.AuditResult) BaselineSnapshot {
	snap := BaselineSnapshot{
		Version:   "1",
		Timestamp: result.Timestamp,
	}

	// WO-47: record lag on active topics too so LAG_INCREASED can fire.
	for _, t := range result.ActiveTopics {
		snap.Topics = append(snap.Topics, TopicSnapshot{Name: t.Name, Status: "active", Lag: t.TotalLag})
	}
	for _, t := range result.StaleTopics {
		snap.Topics = append(snap.Topics, TopicSnapshot{Name: t.Name, Status: "stale", Lag: t.TotalLag})
	}
	for _, t := range result.UnusedTopics {
		snap.Topics = append(snap.Topics, TopicSnapshot{Name: t.Name, Status: "unused"})
	}
	for _, t := range result.ManagedTopics {
		snap.Topics = append(snap.Topics, TopicSnapshot{Name: t.Name, Status: "managed"})
	}
	return snap
}

// WO-48: a topic whose status changed relative to the baseline.
type Delta struct {
	Topic     string `json:"topic"`
	From      string `json:"from"`
	To        string `json:"to"`
	LagFrom   int64  `json:"lag_from,omitempty"`
	LagTo     int64  `json:"lag_to,omitempty"`
	DeltaType string `json:"delta_type"`
}

// WO-48: compute deltas between a baseline and the current result.
//
// Handles three cases: topics whose status changed, topics newly created
// since the baseline (NEWLY_ACTIVE), and topics that were deleted (DELETED).
func computeDeltas(baseline BaselineSnapshot, result *reporter.AuditResult) []Delta {
	prev := make(map[string]TopicSnapshot, len(baseline.Topics))
	for _, t := range baseline.Topics {
		prev[t.Name] = t
	}

	curr := snapshotFromResult(result)
	currMap := make(map[string]bool, len(curr.Topics))

	var deltas []Delta

	for _, c := range curr.Topics {
		currMap[c.Name] = true
		p, existed := prev[c.Name]
		if !existed {
			// New topic since baseline.
			deltas = append(deltas, Delta{
				Topic:     c.Name,
				From:      "absent",
				To:        c.Status,
				LagTo:     c.Lag,
				DeltaType: "NEWLY_ACTIVE",
			})
			continue
		}
		if p.Status == c.Status && p.Lag == c.Lag {
			continue // unchanged
		}

		dt := "STATUS_CHANGE"
		switch {
		case p.Status != "unused" && c.Status == "unused":
			dt = "NEWLY_UNUSED"
		case p.Status == "unused" && c.Status != "unused":
			dt = "NEWLY_ACTIVE"
		case p.Status != "stale" && c.Status == "stale":
			dt = "NEWLY_STALE"
		case c.Lag > p.Lag*2 && p.Lag > 0:
			dt = "LAG_INCREASED"
		}

		deltas = append(deltas, Delta{
			Topic:     c.Name,
			From:      p.Status,
			To:        c.Status,
			LagFrom:   p.Lag,
			LagTo:     c.Lag,
			DeltaType: dt,
		})
	}

	// Topics in baseline but not in current were deleted.
	for name, p := range prev {
		if !currMap[name] {
			deltas = append(deltas, Delta{
				Topic:     name,
				From:      p.Status,
				To:        "absent",
				DeltaType: "DELETED",
			})
		}
	}

	return deltas
}

// WO-48: init creates a default config file for ANCC compliance.
// WO-48: baseline command for snapshot save
func newBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Manage baseline snapshots for delta reporting",
	}

	saveCmd := &cobra.Command{
		Use:   "save [path]",
		Short: "Save the current audit result as a baseline snapshot",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := resolveAuditOptions(cmd, auditOptions{output: "json", lagThreshold: defaultLagThreshold})
			if err != nil {
				return err
			}
			result, err := runAuditForResult(cmd, opts)
			if err != nil {
				return err
			}

			snap := snapshotFromResult(result)
			path := "kafkaspectre-baseline.json"
			if len(args) > 0 {
				path = args[0]
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}
			data, err := json.MarshalIndent(snap, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(abs, data, 0o644); err != nil {
				return fmt.Errorf("write baseline: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved baseline (%d topics) to %s\n", len(snap.Topics), abs)
			return err
		},
	}
	opts := auditOptions{}
	registerConnectionFlags(saveCmd.Flags(), opts.connection())

	cmd.AddCommand(saveCmd)

	return cmd
}

// loadBaseline reads and parses a baseline snapshot file.
func loadBaseline(path string) (BaselineSnapshot, error) {
	var snap BaselineSnapshot
	data, err := os.ReadFile(path)
	if err != nil {
		return snap, fmt.Errorf("read baseline %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return snap, fmt.Errorf("parse baseline %q: %w", path, err)
	}
	return snap, nil
}

// runAuditForResult runs the full audit pipeline and returns the result
// without writing output. Used by baseline save.
func runAuditForResult(cmd *cobra.Command, opts auditOptions) (*reporter.AuditResult, error) {
	conn := opts.connection()
	excludePatterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return nil, err
	}
	output, err := resolvedOutput(opts.output)
	if err != nil {
		return nil, err
	}
	_ = output // not used for baseline
	if err := validateConnection(conn); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.bootstrapServer) == "" {
		return nil, errors.New("bootstrap-server is required")
	}

	kafkaCfg := buildKafkaConfig(conn)

	inspector, err := kafka.NewInspector(kafkaCfg)
	if err != nil {
		return nil, err
	}
	defer inspector.Close()

	ctx, cancel := context.WithTimeout(cmd.Context(), kafkaCfg.QueryTimeout)
	defer cancel()

	metadata, err := inspector.FetchMetadata(ctx)
	if err != nil {
		return nil, err
	}

	result := buildAuditResultWithOptions(metadata, opts.excludeInternal, excludePatterns, opts.includeManaged, opts.lagThreshold)
	result.Tool = "kafkaspectre"
	result.Version = Version
	result.Timestamp = time.Now().UTC().Format(time.RFC3339)

	return result, nil
}
