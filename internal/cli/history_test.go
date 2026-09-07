// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ph-commons/pse-edge-pp-cli/internal/psecal"
	"github.com/ph-commons/pse-edge-pp-cli/internal/store"
)

// TestNovelHistoryHelpWires smoke-tests that the history command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelHistoryHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"history", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("history --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "history"} {
		if !strings.Contains(help, want) {
			t.Fatalf("history --help missing %q in output:\n%s", want, help)
		}
	}
}

func TestAnalyticsDateRange(t *testing.T) {
	asOf := time.Date(2026, 7, 27, 0, 0, 0, 0, psecal.Manila())
	window := 30 * 24 * time.Hour

	from, to, err := analyticsDateRange("", "", asOf, window)
	if err != nil || from != "2026-06-27" || to != "2026-07-27" {
		t.Fatalf("default range = %s..%s (err %v), want 2026-06-27..2026-07-27", from, to, err)
	}

	from, to, err = analyticsDateRange("2020-01-01", "2020-01-31", asOf, window)
	if err != nil || from != "2020-01-01" || to != "2020-01-31" {
		t.Fatalf("explicit range = %s..%s (err %v), want 2020-01-01..2020-01-31", from, to, err)
	}

	// --from alone keeps the as-of end; --to alone anchors the window end.
	from, to, err = analyticsDateRange("2026-01-01", "", asOf, window)
	if err != nil || from != "2026-01-01" || to != "2026-07-27" {
		t.Fatalf("from-only range = %s..%s (err %v), want 2026-01-01..2026-07-27", from, to, err)
	}

	if _, _, err = analyticsDateRange("2026-07-28", "2026-07-27", asOf, window); err == nil {
		t.Fatal("inverted range accepted, want error")
	}
	if _, _, err = analyticsDateRange("28-07-2026", "", asOf, window); err == nil {
		t.Fatal("malformed --from accepted, want error")
	}
}

// ---------------------------------------------------------------------------
// Coverage / stale wrapper tests (issue #32). Seeding is psecal-relative: the
// test computes the same last-completed-trading-day as history.go, so results
// are deterministic regardless of weekday, holiday, or time-of-day.
// ---------------------------------------------------------------------------

// recentTradingDays returns the n most recent PH trading days ending at
// (and including) the last completed trading day, oldest first.
func recentTradingDays(t *testing.T, n int) []string {
	t.Helper()
	last := psecal.LastCompletedTradingDay(time.Now())
	var newest []string
	cur := last
	for len(newest) < n {
		if psecal.IsTradingDay(cur) {
			newest = append(newest, cur.Format("2006-01-02"))
		}
		cur = cur.AddDate(0, 0, -1)
	}
	out := make([]string, len(newest))
	for i := range newest {
		out[len(newest)-1-i] = newest[i]
	}
	return out
}

func seedHistoryStore(t *testing.T, sym string, dates []string) *store.Store {
	t.Helper()
	db, err := store.OpenWithContext(context.Background(), filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsurePSEEdgeTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}
	for _, d := range dates {
		if _, err := db.DB().Exec(
			`INSERT INTO pse_eod_prices(symbol, trading_date, open, high, low, close, value, volume, source)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			sym, d, 10.0, 11.0, 9.5, 10.5, 1000000.0, 500000.0, "edge",
		); err != nil {
			t.Fatalf("seed row %s: %v", d, err)
		}
	}
	return db
}

func runHistory(t *testing.T, dbPath string, args ...string) (string, int) {
	t.Helper()
	cmd := RootCmd()
	full := append([]string{"history"}, args...)
	full = append(full, "--db", dbPath)
	cmd.SetArgs(full)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	err := cmd.Execute()
	code := 0
	if err != nil {
		var typed *cliError
		if errors.As(err, &typed) {
			code = typed.code
		} else {
			code = 1
		}
	}
	return out.String(), code
}

func decodeHistoryWrapper(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON wrapper: %v\n%s", err, out)
	}
	return m
}

func TestHistoryCoverage_CoveredSeries(t *testing.T) {
	days := recentTradingDays(t, 5)
	db := seedHistoryStore(t, "AT", days)
	last := days[len(days)-1]

	out, code := runHistory(t, db.Path(), "AT", "--since", "30d", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if m["stale"] != false {
		t.Errorf("stale = %v, want false (series fully covered)", m["stale"])
	}
	if m["session_last_completed"] != last {
		t.Errorf("session_last_completed = %v, want %s", m["session_last_completed"], last)
	}
	cov, ok := m["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage missing/not object: %v", m["coverage"])
	}
	if cov["last"] != last {
		t.Errorf("coverage.last = %v, want %s", cov["last"], last)
	}
	bars, ok := m["bars"].([]any)
	if !ok || len(bars) != 5 {
		t.Errorf("bars = %v, want 5 rows", m["bars"])
	}
	if _, present := m["sync_required"]; present {
		t.Errorf("sync_required should be absent on a covered series, got %v", m["sync_required"])
	}
	if _, uncovered := m["calendar_coverage"]; !uncovered {
		gaps, ok := cov["gaps"].([]any)
		if !ok {
			t.Errorf("gaps = %v, want [] on a calendar-covered series", cov["gaps"])
		} else if len(gaps) != 0 {
			t.Errorf("gaps = %v, want empty list (no internal holes)", gaps)
		}
	}
}

func TestHistoryCoverage_StaleSeries(t *testing.T) {
	days := recentTradingDays(t, 5)
	db := seedHistoryStore(t, "AT", days[:3])
	last := days[len(days)-1]
	coverageLast := days[2]

	out, code := runHistory(t, db.Path(), "AT", "--since", "30d", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stale is a data signal, not an error); out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if m["stale"] != true {
		t.Errorf("stale = %v, want true", m["stale"])
	}
	cov, _ := m["coverage"].(map[string]any)
	if cov["last"] != coverageLast {
		t.Errorf("coverage.last = %v, want %s", cov["last"], coverageLast)
	}
	if m["session_last_completed"] != last {
		t.Errorf("session_last_completed = %v, want %s", m["session_last_completed"], last)
	}
	if _, present := m["sync_required"]; present {
		t.Errorf("sync_required should be absent (series has data), got %v", m["sync_required"])
	}
}

func TestHistoryCoverage_InternalHoleAndNoTrailingGap(t *testing.T) {
	days := recentTradingDays(t, 5)
	hole := days[3]
	seeded := []string{days[0], days[1], days[2], days[4]}
	db := seedHistoryStore(t, "AT", seeded)

	out, code := runHistory(t, db.Path(), "AT", "--since", "30d", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	cov, _ := m["coverage"].(map[string]any)
	if m["stale"] != false {
		t.Errorf("stale = %v, want false (series reaches last completed)", m["stale"])
	}
	// Gap detection is calendar-gated: within the holiday-table years the
	// internal hole must be listed; outside those years gaps is null by
	// contract (the best-effort calendar would fabricate holes). Both are
	// deterministic — never assert a list unconditionally.
	if _, uncovered := m["calendar_coverage"]; uncovered {
		if cov["gaps"] != nil {
			t.Errorf("gaps = %v, want null when calendar not covered", cov["gaps"])
		}
		return
	}
	gaps, ok := cov["gaps"].([]any)
	if !ok {
		t.Fatalf("gaps not a list: %v", cov["gaps"])
	}
	if len(gaps) != 1 || gaps[0] != hole {
		t.Errorf("gaps = %v, want exactly [%s] (internal hole, no trailing gaps)", gaps, hole)
	}
}

func TestHistoryCoverage_WeekendEndBoundary(t *testing.T) {
	days := recentTradingDays(t, 5)
	db := seedHistoryStore(t, "AT", days)
	last := days[len(days)-1]

	// --to the day after the last completed session (may be a weekend/holiday)
	// must not fabricate a stale flag or trailing gaps.
	lastT, err := time.ParseInLocation("2006-01-02", last, psecal.Manila())
	if err != nil {
		t.Fatal(err)
	}
	next := lastT.AddDate(0, 0, 1).Format("2006-01-02")

	out, code := runHistory(t, db.Path(), "AT", "--from", days[0], "--to", next, "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if m["stale"] != false {
		t.Errorf("stale = %v, want false for a fully-synced series queried to %s", m["stale"], next)
	}
	cov, _ := m["coverage"].(map[string]any)
	if _, uncovered := m["calendar_coverage"]; !uncovered {
		if gaps, ok := cov["gaps"].([]any); ok && len(gaps) != 0 {
			t.Errorf("gaps = %v, want empty (trailing non-trading days are not holes)", gaps)
		}
	}
	bars, _ := m["bars"].([]any)
	if len(bars) != 5 {
		t.Errorf("bars = %d rows, want 5 (all seeded rows within window)", len(bars))
	}
}

func TestHistoryCoverage_NeverSyncedNoDBFile(t *testing.T) {
	out, code := runHistory(t, filepath.Join(t.TempDir(), "does-not-exist.db"), "AT", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (no-db never-synced stays exit 0)", code)
	}
	m := decodeHistoryWrapper(t, out)
	if m["sync_required"] != true {
		t.Errorf("sync_required = %v, want true", m["sync_required"])
	}
	if m["stale"] != true {
		t.Errorf("stale = %v, want true", m["stale"])
	}
	bars, _ := m["bars"].([]any)
	if len(bars) != 0 {
		t.Errorf("bars = %v, want []", m["bars"])
	}
	cov, _ := m["coverage"].(map[string]any)
	if cov["first"] != nil || cov["last"] != nil || cov["gaps"] != nil {
		t.Errorf("coverage = %v, want null first/last/gaps", cov)
	}
}

func TestHistoryCoverage_NeverSyncedZeroRows(t *testing.T) {
	db := seedHistoryStore(t, "ZZ", []string{recentTradingDays(t, 1)[0]})
	out, code := runHistory(t, db.Path(), "AT", "--json")
	if code != 3 {
		t.Fatalf("exit code = %d, want 3 (never-synced symbol); out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if m["sync_required"] != true {
		t.Errorf("sync_required = %v, want true", m["sync_required"])
	}
	cov, _ := m["coverage"].(map[string]any)
	if cov["first"] != nil || cov["last"] != nil {
		t.Errorf("coverage = %v, want null bounds", cov)
	}
}

func TestHistoryCoverage_NonOverlapInYearWindow(t *testing.T) {
	// Series in June 2026; window in January 2026 — calendar-covered, no
	// intersection. gaps must be [] (not null): null is reserved for empty
	// series / disabled gap detection (historyGapsWithin start>end).
	db := seedHistoryStore(t, "AT", []string{"2026-06-01", "2026-06-02", "2026-06-03"})
	out, code := runHistory(t, db.Path(), "AT", "--from", "2026-01-05", "--to", "2026-01-09", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if _, uncovered := m["calendar_coverage"]; uncovered {
		t.Fatalf("calendar_coverage present on an in-year 2026 window: %v", m["calendar_coverage"])
	}
	cov, ok := m["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage missing/not object: %v", m["coverage"])
	}
	gaps, ok := cov["gaps"].([]any)
	if !ok {
		t.Fatalf("gaps = %v, want [] (calendar-covered, empty intersection)", cov["gaps"])
	}
	if len(gaps) != 0 {
		t.Errorf("gaps = %v, want empty list", gaps)
	}
	if cov["first"] != "2026-06-01" || cov["last"] != "2026-06-03" {
		t.Errorf("coverage bounds = %v..%v, want 2026-06-01..2026-06-03", cov["first"], cov["last"])
	}
	bars, _ := m["bars"].([]any)
	if len(bars) != 0 {
		t.Errorf("bars = %d rows, want 0 (window does not overlap series)", len(bars))
	}
}

func TestHistoryCoverage_Pre2026CalendarGuard(t *testing.T) {
	db := seedHistoryStore(t, "AT", []string{"2021-12-24"})
	out, code := runHistory(t, db.Path(), "AT", "--from", "2021-01-01", "--to", "2021-12-31", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	cov, _ := m["coverage"].(map[string]any)
	if cov["gaps"] != nil {
		t.Errorf("gaps = %v, want null outside calendar-covered years (no fabricated holes)", cov["gaps"])
	}
	cc, ok := m["calendar_coverage"].(map[string]any)
	if !ok {
		t.Fatalf("calendar_coverage missing on out-of-year window: %v", m)
	}
	if cc["covered"] != false {
		t.Errorf("calendar_coverage.covered = %v, want false", cc["covered"])
	}
	if cc["min_year"] != float64(2026) || cc["max_year"] != float64(2026) {
		t.Errorf("calendar_coverage bounds = %v/%v, want 2026/2026", cc["min_year"], cc["max_year"])
	}
}

func TestHistoryCoverage_CSVStillRows(t *testing.T) {
	days := recentTradingDays(t, 3)
	db := seedHistoryStore(t, "AT", days)
	out, code := runHistory(t, db.Path(), "AT", "--since", "30d", "--csv")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	if strings.Contains(out, "coverage") || strings.Contains(out, "session_last_completed") {
		t.Errorf("csv output leaked wrapper metadata:\n%s", out)
	}
	if strings.Contains(out, "\"bars\"") {
		t.Errorf("csv output leaked wrapper object:\n%s", out)
	}
	if !strings.Contains(out, days[0]) {
		t.Errorf("csv output missing seeded date %s:\n%s", days[0], out)
	}
}

func TestHistoryCoverage_SelectKeepsEnvelope(t *testing.T) {
	days := recentTradingDays(t, 3)
	db := seedHistoryStore(t, "AT", days)
	out, code := runHistory(t, db.Path(), "AT", "--since", "30d", "--json", "--select", "date,close")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if _, ok := m["coverage"]; !ok {
		t.Errorf("coverage stripped by --select:\n%s", out)
	}
	if _, ok := m["session_last_completed"]; !ok {
		t.Errorf("session_last_completed stripped by --select:\n%s", out)
	}
	bars, ok := m["bars"].([]any)
	if !ok {
		t.Fatalf("bars not present after --select:\n%s", out)
	}
	first := bars[0].(map[string]any)
	if _, hasDate := first["date"]; !hasDate {
		t.Errorf("bar missing selected field date: %v", first)
	}
	if _, hasVol := first["volume"]; hasVol {
		t.Errorf("bar leaked non-selected field volume: %v", first)
	}
}

func TestHistoryCoverage_AgentShape(t *testing.T) {
	days := recentTradingDays(t, 3)
	db := seedHistoryStore(t, "AT", days)
	out, code := runHistory(t, db.Path(), "AT", "--since", "30d", "--agent")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("--agent output not JSON: %v\n%s", err, out)
	}
	if _, ok := env["meta"]; !ok {
		t.Errorf("--agent output missing meta envelope:\n%s", out)
	}
	results, ok := env["results"].(map[string]any)
	if !ok {
		t.Fatalf("--agent results not an object (wrapper nested): %v\n%s", env["results"], out)
	}
	if _, ok := results["coverage"]; !ok {
		t.Errorf("--agent results missing coverage:\n%s", out)
	}
	if _, ok := results["bars"]; !ok {
		t.Errorf("--agent results missing bars:\n%s", out)
	}
}

func TestHistoryCoverage_IndexParity(t *testing.T) {
	days := recentTradingDays(t, 3)
	db, err := store.OpenWithContext(context.Background(), filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsurePSEEdgeTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}
	for _, d := range days {
		if _, err := db.DB().Exec(
			`INSERT INTO pse_index_snapshots(index_code, trading_date, value, source) VALUES (?,?,?,?)`,
			"PSEI", d, 7000.0, "edge",
		); err != nil {
			t.Fatalf("seed index row %s: %v", d, err)
		}
	}

	out, code := runHistory(t, db.Path(), "PSEI", "--since", "30d", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out: %s", code, out)
	}
	m := decodeHistoryWrapper(t, out)
	if m["stale"] != false {
		t.Errorf("stale = %v, want false", m["stale"])
	}
	if _, ok := m["coverage"].(map[string]any); !ok {
		t.Errorf("coverage missing on index path: %v", m["coverage"])
	}
	bars, _ := m["bars"].([]any)
	if len(bars) != 3 {
		t.Errorf("bars = %d rows, want 3", len(bars))
	}
}
