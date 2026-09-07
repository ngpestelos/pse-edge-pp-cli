// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStreamExportEOD(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsurePSEEdgeTables(ctx); err != nil {
		t.Fatal(err)
	}
	vol := 1000.0
	if err := s.UpsertPSEEODPrices(ctx, []PSEEODRow{
		{Symbol: "AT", TradingDate: "2026-01-02", Open: 1, High: 2, Low: 1, Close: 1.5, Value: 1e6, Volume: &vol, Source: "edge"},
		{Symbol: "AT", TradingDate: "2026-01-03", Open: 1.5, High: 2, Low: 1.4, Close: 1.8, Value: 2e6, Source: "edge"},
		{Symbol: "GTCAP", TradingDate: "2026-01-02", Open: 10, High: 11, Low: 9, Close: 10.5, Value: 3e6, Source: "edge"},
	}); err != nil {
		t.Fatal(err)
	}

	var rows []ExportEODRow
	n, err := s.StreamExportEOD(ctx, "2026-01-02", "2026-01-03", []string{"AT"}, 0, func(r ExportEODRow) error {
		rows = append(rows, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(rows) != 2 {
		t.Fatalf("n=%d rows=%d", n, len(rows))
	}
	if rows[0].Contract != ExportContractEOD || rows[0].Symbol != "AT" {
		t.Fatalf("row0 = %+v", rows[0])
	}
	if rows[0].Volume == nil || *rows[0].Volume != 1000 {
		t.Fatalf("volume = %v", rows[0].Volume)
	}
	if rows[0].VolumeStatus != "ok" {
		t.Fatalf("row0 volume_status = %q, want ok", rows[0].VolumeStatus)
	}
	if rows[1].Volume != nil {
		t.Fatalf("second volume want nil, got %v", rows[1].Volume)
	}
	if rows[1].VolumeStatus != "unavailable" {
		t.Fatalf("row1 volume_status = %q, want unavailable", rows[1].VolumeStatus)
	}
}

func TestStreamExportIndex(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsurePSEEdgeTables(ctx); err != nil {
		t.Fatal(err)
	}
	var adv int64 = 100
	if err := s.UpsertPSEIndexSnapshots(ctx, []PSEIndexSnapshotRow{
		{IndexCode: "PSEI", TradingDate: "2026-01-02", Value: 6000, Advances: &adv, Source: "edge"},
	}); err != nil {
		t.Fatal(err)
	}
	var rows []ExportIndexRow
	n, err := s.StreamExportIndex(ctx, "2026-01-01", "2026-12-31", nil, 0, func(r ExportIndexRow) error {
		rows = append(rows, r)
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if rows[0].Contract != ExportContractIndex || rows[0].Advances == nil || *rows[0].Advances != 100 {
		t.Fatalf("%+v", rows[0])
	}
}
