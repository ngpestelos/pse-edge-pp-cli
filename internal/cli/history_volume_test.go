// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ph-commons/pse-edge-pp-cli/internal/store"
)

func TestHistoryVolumeStatus(t *testing.T) {
	if got := volumeStatus(nil); got != "unavailable" {
		t.Fatalf("nil volume status = %q, want unavailable", got)
	}
	zero := 0.0
	if got := volumeStatus(&zero); got != "ok" {
		t.Fatalf("zero volume status = %q, want ok", got)
	}
	n := 1000.0
	if got := volumeStatus(&n); got != "ok" {
		t.Fatalf("nonzero volume status = %q, want ok", got)
	}

	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsurePSEEdgeTables(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPSEEODPrices(ctx, []store.PSEEODRow{
		{Symbol: "AT", TradingDate: "2026-01-02", Open: 1, High: 2, Low: 1, Close: 1.5, Value: 1e6, Volume: &n, Source: "edge"},
		{Symbol: "AT", TradingDate: "2026-01-03", Open: 1.5, High: 2, Low: 1.4, Close: 1.8, Value: 2e6, Source: "edge"},
		{Symbol: "AT", TradingDate: "2026-01-06", Open: 1.8, High: 2, Low: 1.5, Close: 1.6, Value: 0, Volume: &zero, Source: "edge"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPSEIndexSnapshots(ctx, []store.PSEIndexSnapshotRow{
		{IndexCode: "PSEI", TradingDate: "2026-01-02", Value: 6000, Source: "edge"},
	}); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	symRows, err := historySymbolRows(cmd, s, "AT", "2026-01-01", "2026-01-31", "2026-01-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(symRows) != 3 {
		t.Fatalf("symbol rows = %d, want 3", len(symRows))
	}
	assertHistoryVolumeJSON(t, symRows[0], 1000.0, "ok")
	assertHistoryVolumeJSON(t, symRows[1], nil, "unavailable")
	assertHistoryVolumeJSON(t, symRows[2], 0.0, "ok")

	idxRows, err := historyIndexRows(cmd, s, "PSEI", "2026-01-01", "2026-01-31", "2026-01-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(idxRows) != 1 {
		t.Fatalf("index rows = %d, want 1", len(idxRows))
	}
	assertHistoryVolumeJSON(t, idxRows[0], nil, "unavailable")

	helpCmd := RootCmd()
	var out bytes.Buffer
	helpCmd.SetOut(&out)
	helpCmd.SetErr(&out)
	helpCmd.SetArgs([]string{"history", "--help"})
	if err := helpCmd.Execute(); err != nil {
		t.Fatalf("history --help: %v", err)
	}
	help := out.String()
	for _, want := range []string{"DisclosureCht", "volume_status", "null"} {
		if !strings.Contains(help, want) {
			t.Fatalf("history --help missing %q:\n%s", want, help)
		}
	}
}

func assertHistoryVolumeJSON(t *testing.T, row historyRow, wantVolume any, wantStatus string) {
	t.Helper()
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["volume"]; !ok {
		t.Fatalf("volume omitted from JSON: %s", b)
	}
	if m["volume"] != wantVolume {
		t.Fatalf("volume = %v, want %v in %s", m["volume"], wantVolume, b)
	}
	if m["volume_status"] != wantStatus {
		t.Fatalf("volume_status = %v, want %q in %s", m["volume_status"], wantStatus, b)
	}
	if row.VolumeStatus != wantStatus {
		t.Fatalf("VolumeStatus = %q, want %q", row.VolumeStatus, wantStatus)
	}
}
