// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command helper — kept out of generated files (mirrors pse_common.go) so
// `generate --force` never overwrites it.

package cli

import (
	"database/sql"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/ph-commons/pse-edge-pp-cli/internal/psecal"
	"github.com/ph-commons/pse-edge-pp-cli/internal/store"
)

// historyCoverage is the machine-readable coverage span for a history query.
// First/Last are the global series bounds; Gaps lists internal holes only —
// expected trading days inside [first,last] that carry no bar. Trailing
// unsynced sessions and non-trading days never appear (the span is bounded by
// real data), so "series stale" and "internal hole" stay distinct. Gaps is
// null when the series is empty or gap detection is disabled (window outside
// the calendar's known holiday years); otherwise it is a (possibly empty) list.
type historyCoverage struct {
	First *string  `json:"first"`
	Last  *string  `json:"last"`
	Gaps  []string `json:"gaps"`
	Total int      `json:"-"`
}

// historyCalendarCoverage reports the holiday-table year bounds relevant to a
// history window. When the window falls outside these years, gap detection is
// disabled (the best-effort holiday table would fabricate holes) and the bounds
// are surfaced instead of silently assumed.
type historyCalendarCoverage struct {
	MinYear int  `json:"min_year"`
	MaxYear int  `json:"max_year"`
	Covered bool `json:"covered"`
}

// historyResult is the wrapper object emitted by `history` under --json/--agent.
// The wrapper carries the machine-readable coverage/stale signal so callers can
// distinguish "no data" from "not synced" (issue #32).
type historyResult struct {
	Bars                 []historyRow             `json:"bars"`
	Coverage             historyCoverage          `json:"coverage"`
	SessionLastCompleted string                   `json:"session_last_completed"`
	Stale                bool                     `json:"stale"`
	SyncRequired         bool                     `json:"sync_required,omitempty"`
	CalendarCoverage     *historyCalendarCoverage `json:"calendar_coverage,omitempty"`
}

// historyCoverageFor computes the global series coverage (MIN/MAX span plus
// internal gaps) for a keyed series in the given table. ok is false when the
// series has no rows (never synced). The calendar-coverage guard is attached
// only when the requested window is outside the holiday-table years. Real DB
// errors are propagated (never misread as "never synced").
func historyCoverageFor(cmd *cobra.Command, db *store.Store, table, keyCol, key, from, to string) (historyCoverage, bool, *historyCalendarCoverage, error) {
	spanQuery := "SELECT COUNT(*), MIN(trading_date), MAX(trading_date) FROM " + table + " WHERE " + keyCol + " = ?"
	var total int
	var first, last sql.NullString
	if err := db.DB().QueryRowContext(cmd.Context(), spanQuery, key).Scan(&total, &first, &last); err != nil {
		if syncHintMissingTable(err) {
			return historyCoverage{}, false, nil, nil
		}
		return historyCoverage{}, false, nil, err
	}
	if total == 0 || !first.Valid || !last.Valid {
		return historyCoverage{}, false, nil, nil
	}

	cov := historyCoverage{
		First: &first.String,
		Last:  &last.String,
		Total: total,
	}
	gaps, err := historyGapsWithin(cmd, db, table, keyCol, key, from, to, first.String, last.String)
	if err != nil {
		return historyCoverage{}, false, nil, err
	}
	cov.Gaps = gaps
	var cc *historyCalendarCoverage
	if !historyWindowCalendarCovered(from, to) {
		cc = historyWindowCalendarCoverage(from, to)
		cov.Gaps = nil
	}
	return cov, true, cc, nil
}

// historyGapsWithin lists expected trading days inside the requested window
// that are bounded by the series' real-data span and carry no bar. Returns an
// empty (non-nil) list when the covered series has no internal holes, and nil
// when the window is not calendar-covered (gap detection would fabricate holes
// outside the best-effort holiday table's years). Real DB errors propagate.
func historyGapsWithin(cmd *cobra.Command, db *store.Store, table, keyCol, key, from, to, first, last string) ([]string, error) {
	if !historyWindowCalendarCovered(from, to) {
		return nil, nil
	}
	start := maxDateKey(from, first)
	end := minDateKey(to, last)
	if start == "" || end == "" || start > end {
		return make([]string, 0), nil
	}

	barDates := map[string]bool{}
	rows, err := db.DB().QueryContext(cmd.Context(),
		"SELECT trading_date FROM "+table+" WHERE "+keyCol+" = ? AND trading_date BETWEEN ? AND ?",
		key, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, err
		}
		barDates[d] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	cur, err := time.ParseInLocation("2006-01-02", start, psecal.Manila())
	if err != nil {
		return nil, err
	}
	endT, err := time.ParseInLocation("2006-01-02", end, psecal.Manila())
	if err != nil {
		return nil, err
	}
	gaps := make([]string, 0)
	for !cur.After(endT) {
		key := cur.Format("2006-01-02")
		if psecal.IsTradingDay(cur) && !barDates[key] {
			gaps = append(gaps, key)
		}
		cur = cur.AddDate(0, 0, 1)
	}
	return gaps, nil
}

// historyWindowCalendarCovered reports whether the requested window is fully
// inside the holiday-table year bounds, disabling gap detection otherwise.
func historyWindowCalendarCovered(from, to string) bool {
	cc := historyWindowCalendarCoverage(from, to)
	return cc.Covered
}

// historyWindowCalendarCoverage surfaces the holiday-table year bounds for a
// window, marking covered=false when the window is not fully inside them.
func historyWindowCalendarCoverage(from, to string) *historyCalendarCoverage {
	minY, maxY, _ := psecal.CalendarCoverage(time.Now())
	cc := &historyCalendarCoverage{MinYear: minY, MaxYear: maxY}
	if len(from) < 4 || len(to) < 4 {
		return cc
	}
	fromY, err1 := strconv.Atoi(from[:4])
	toY, err2 := strconv.Atoi(to[:4])
	if err1 != nil || err2 != nil {
		return cc
	}
	cc.Covered = fromY >= minY && toY <= maxY
	return cc
}

// emitHistoryResult renders a history query result. The coverage wrapper is a
// --json/--agent contract: human/table, --csv, and --plain consumers render the
// bars array (rows), never the wrapper, so the default output shape is
// unchanged.
func emitHistoryResult(w io.Writer, res historyResult, flags *rootFlags) error {
	barsRaw, err := json.Marshal(res.Bars)
	if err != nil {
		return err
	}
	if !flags.asJSON || flags.csv || flags.plain {
		return printOutputWithFlags(w, barsRaw, flags)
	}
	wrapperRaw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return printOutputWithFlags(w, wrapperRaw, flags)
}

// maxDateKey returns the later of two YYYY-MM-DD keys (lexicographic compare
// is correct for the fixed-width format). Empty inputs never win.
func maxDateKey(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a > b {
		return a
	}
	return b
}

// minDateKey returns the earlier of two YYYY-MM-DD keys (empty inputs lose).
func minDateKey(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a < b {
		return a
	}
	return b
}
