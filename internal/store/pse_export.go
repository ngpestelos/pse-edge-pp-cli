// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
//
// Read paths for the versioned local-store export contract (issue #9).
// Downstream consumers should prefer CLI `export eod|index|companies-local`
// over opening data.db directly — field names and nullability here are the
// stability promise, not the physical SQLite layout.

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Export contract identifiers. Bump the version suffix only when removing or
// renaming a field (additive nullable columns may stay on the same version
// with a CHANGELOG note).
const (
	ExportContractEOD       = "pse-edge-export-eod-v1"
	ExportContractIndex     = "pse-edge-export-index-v1"
	ExportContractCompanies = "pse-edge-export-companies-v1"
)

// ExportEODRow is one JSONL object for `export eod` (pse-edge-export-eod-v1).
type ExportEODRow struct {
	Contract     string   `json:"contract"`
	Symbol       string   `json:"symbol"`
	TradingDate  string   `json:"trading_date"`
	Open         float64  `json:"open"`
	High         float64  `json:"high"`
	Low          float64  `json:"low"`
	Close        float64  `json:"close"`
	Value        float64  `json:"value"`
	Volume       *float64 `json:"volume"` // null when DisclosureCht.ax had no share volume
	VolumeStatus string   `json:"volume_status"`
	Source       string   `json:"source"`
}

// volumeStatus reports whether a share-count pointer is present. Nil is
// unavailable; a non-nil value, including 0, is ok. Never impute zeros.
func volumeStatus(volume *float64) string {
	if volume == nil {
		return "unavailable"
	}
	return "ok"
}

// ExportIndexRow is one JSONL object for `export index` (pse-edge-export-index-v1).
type ExportIndexRow struct {
	Contract    string   `json:"contract"`
	IndexCode   string   `json:"index_code"`
	TradingDate string   `json:"trading_date"`
	Value       float64  `json:"value"`
	Change      *float64 `json:"change"`
	PctChange   *float64 `json:"pct_change"`
	Advances    *int     `json:"advances"`
	Declines    *int     `json:"declines"`
	Unchanged   *int     `json:"unchanged"`
	TotalVolume *float64 `json:"total_volume"`
	TotalValue  *float64 `json:"total_value"`
	TotalTrades *int     `json:"total_trades"`
	Source      string   `json:"source"`
}

// ExportCompanyRow is one JSONL object for `export companies-local`.
type ExportCompanyRow struct {
	Contract   string `json:"contract"`
	CmpyID     int    `json:"cmpy_id"`
	SecurityID int    `json:"security_id"`
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	ETF        bool   `json:"etf"`
	SyncedAt   string `json:"synced_at,omitempty"`
}

// StreamExportEOD writes rows for symbols (empty = all) in [from,to] YYYY-MM-DD
// ascending (symbol, trading_date). limit 0 = unlimited. emit is called once
// per row; return a non-nil error to stop.
func (s *Store) StreamExportEOD(ctx context.Context, from, to string, symbols []string, limit int, emit func(ExportEODRow) error) (int, error) {
	if err := ensurePSEEdgeTablesRO(ctx, s); err != nil {
		return 0, err
	}
	q := strings.Builder{}
	q.WriteString(`SELECT symbol, trading_date, open, high, low, close, value, volume, source
		FROM pse_eod_prices WHERE 1=1`)
	args := make([]any, 0, 8)
	if from != "" {
		q.WriteString(` AND trading_date >= ?`)
		args = append(args, from)
	}
	if to != "" {
		q.WriteString(` AND trading_date <= ?`)
		args = append(args, to)
	}
	if len(symbols) > 0 {
		q.WriteString(` AND symbol IN (`)
		for i, sym := range symbols {
			if i > 0 {
				q.WriteByte(',')
			}
			q.WriteByte('?')
			args = append(args, sym)
		}
		q.WriteByte(')')
	}
	q.WriteString(` ORDER BY symbol, trading_date`)
	if limit > 0 {
		q.WriteString(` LIMIT ?`)
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("export eod: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var r ExportEODRow
		var vol sql.NullFloat64
		if err := rows.Scan(&r.Symbol, &r.TradingDate, &r.Open, &r.High, &r.Low, &r.Close, &r.Value, &vol, &r.Source); err != nil {
			return n, err
		}
		r.Contract = ExportContractEOD
		if vol.Valid {
			v := vol.Float64
			r.Volume = &v
		}
		r.VolumeStatus = volumeStatus(r.Volume)
		if err := emit(r); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// StreamExportIndex writes index snapshot rows in [from,to]. codes empty = all.
func (s *Store) StreamExportIndex(ctx context.Context, from, to string, codes []string, limit int, emit func(ExportIndexRow) error) (int, error) {
	if err := ensurePSEEdgeTablesRO(ctx, s); err != nil {
		return 0, err
	}
	q := strings.Builder{}
	q.WriteString(`SELECT index_code, trading_date, value, change, pct_change,
		advances, declines, unchanged, total_volume, total_value, total_trades, source
		FROM pse_index_snapshots WHERE 1=1`)
	args := make([]any, 0, 8)
	if from != "" {
		q.WriteString(` AND trading_date >= ?`)
		args = append(args, from)
	}
	if to != "" {
		q.WriteString(` AND trading_date <= ?`)
		args = append(args, to)
	}
	if len(codes) > 0 {
		q.WriteString(` AND index_code IN (`)
		for i, c := range codes {
			if i > 0 {
				q.WriteByte(',')
			}
			q.WriteByte('?')
			args = append(args, c)
		}
		q.WriteByte(')')
	}
	q.WriteString(` ORDER BY index_code, trading_date`)
	if limit > 0 {
		q.WriteString(` LIMIT ?`)
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("export index: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var r ExportIndexRow
		var chg, pct, tvol, tval sql.NullFloat64
		var adv, dec, unc, trades sql.NullInt64
		if err := rows.Scan(&r.IndexCode, &r.TradingDate, &r.Value, &chg, &pct, &adv, &dec, &unc, &tvol, &tval, &trades, &r.Source); err != nil {
			return n, err
		}
		r.Contract = ExportContractIndex
		r.Change = nullF64(chg)
		r.PctChange = nullF64(pct)
		r.Advances = nullI(adv)
		r.Declines = nullI(dec)
		r.Unchanged = nullI(unc)
		r.TotalVolume = nullF64(tvol)
		r.TotalValue = nullF64(tval)
		r.TotalTrades = nullI(trades)
		if err := emit(r); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// StreamExportCompanies writes every local registry row.
func (s *Store) StreamExportCompanies(ctx context.Context, limit int, emit func(ExportCompanyRow) error) (int, error) {
	if err := ensurePSEEdgeTablesRO(ctx, s); err != nil {
		return 0, err
	}
	q := `SELECT cmpy_id, COALESCE(security_id, 0), symbol, COALESCE(name, ''), COALESCE(etf, 0), COALESCE(synced_at, '')
		FROM pse_companies WHERE symbol IS NOT NULL AND symbol != '' ORDER BY symbol`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("export companies-local: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var r ExportCompanyRow
		var etf int
		if err := rows.Scan(&r.CmpyID, &r.SecurityID, &r.Symbol, &r.Name, &etf, &r.SyncedAt); err != nil {
			return n, err
		}
		r.Contract = ExportContractCompanies
		r.ETF = etf != 0
		if err := emit(r); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func nullF64(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func nullI(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// ensurePSEEdgeTablesRO creates tables if missing so empty export is [] not error.
func ensurePSEEdgeTablesRO(ctx context.Context, s *Store) error {
	// Prefer Ensure when the store is writable so first export after install
	// does not fail with "no such table". Read-only opens skip create; empty
	// result is then returned via isMissingTable.
	_ = s.EnsurePSEEdgeTables(ctx)
	return nil
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table")
}
