// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command: implemented (was a generated scaffold).
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.

// pp:data-source local

package cli

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ph-commons/pse-edge-pp-cli/internal/psecal"
	"github.com/ph-commons/pse-edge-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// historyRow is one daily bar (or index reading) from the local store.
// Open/high/low are omitted on index rows: the embedded PSEi series
// backfill carries closes only, and fabricating OHLC would violate the
// absence-of-correctness rule. Every row carries source/as_of/stale per
// the red-team output contract; as_of is the last completed PH trading
// day at query time and stale flags a series whose newest bar predates it.
type historyRow struct {
	Date         string   `json:"date"`
	Open         *float64 `json:"open,omitempty"`
	High         *float64 `json:"high,omitempty"`
	Low          *float64 `json:"low,omitempty"`
	Close        float64  `json:"close"`
	Value        float64  `json:"value"`
	Volume       *float64 `json:"volume"`
	VolumeStatus string   `json:"volume_status"`
	Source       string   `json:"source"`
	AsOf         string   `json:"as_of"`
	Stale        bool     `json:"stale"`
}

// volumeStatus reports whether a share-count pointer is present. Nil is
// unavailable (DisclosureCht has no share volume); a non-nil value,
// including 0, is ok. Never impute zeros.
func volumeStatus(volume *float64) string {
	if volume == nil {
		return "unavailable"
	}
	return "ok"
}

func newNovelHistoryCmd(flags *rootFlags) *cobra.Command {
	var flagSince string
	var flagFrom string
	var flagTo string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "history <SYMBOL|INDEX>",
		Short: "Query daily OHLC/value history for any ticker or the PSEi from the local store — data no free PSE API serves.",
		Long: `Use this command for raw daily OHLC/value rows over a range. Do NOT use it for percent-change or index-relative performance; use 'drift' instead.

Reads pse_eod_prices for ticker symbols and pse_index_snapshots for index
codes (PSEI and the sector indices already synced locally). Index rows carry
close/value only — the embedded PSEi backfill series has no OHLC, and this
command never fabricates fields it does not have. Share volume is often
null because DisclosureCht.ax has no share count; a JSON null is not a
zero print. Read volume_status ("ok" when the local column is present,
including 0; "unavailable" when volume is null). Index history has no
share volume, so volume_status is always unavailable. Rows are ascending by
date; weekends and holidays are naturally absent (the store only holds
completed trading sessions).

The window defaults to --since 30d ending at the last completed PH trading
day; --from/--to (YYYY-MM-DD) override it. A range with no local rows
prints [] plus a stderr note stating actual local coverage.`,
		Example: `  pse-edge-pp-cli history AT --since 30d --json
  pse-edge-pp-cli history PSEI --since 30d --json
  pse-edge-pp-cli history AT --from 2026-06-01 --to 2026-06-30 --json`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("history takes exactly one symbol or index code, got %d arguments", len(args)))
			}
			sym, err := validatePSESymbol(args[0])
			if err != nil {
				return err
			}

			window, err := parseSinceWindow(flagSince, 30*24*time.Hour)
			if err != nil {
				return err
			}

			asOfDay := psecal.LastCompletedTradingDay(time.Now())
			asOf := asOfDay.Format("2006-01-02")
			from, to, err := analyticsDateRange(flagFrom, flagTo, asOfDay, window)
			if err != nil {
				return usageErr(err)
			}

			// Missing-mirror guard: no database file means nothing has ever
			// been synced. Hint on stderr, empty result on stdout (history's
			// success shape is an array, so [] stays the empty result here).
			var missing bool
			if dbPath, missing = missingMirrorGuard(cmd, dbPath); missing {
				return printJSONFiltered(cmd.OutOrStdout(), []historyRow{}, flags)
			}

			db, err := store.OpenReadOnlyContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			isIndex, err := analyticsIsIndexCode(cmd, db, sym)
			if err != nil {
				return err
			}

			resource := "pse_eod_prices"
			if isIndex {
				resource = "pse_index_snapshots"
			}
			hintIfUnsynced(cmd, db, resource)
			hintIfStale(cmd, db, resource, 72*time.Hour)

			if isIndex {
				rows, err := historyIndexRows(cmd, db, sym, from, to, asOf)
				if err != nil {
					return err
				}
				if len(rows) == 0 {
					historyEmptyNote(cmd, db, `SELECT MIN(trading_date), MAX(trading_date), COUNT(*) FROM pse_index_snapshots WHERE index_code = ?`, sym, from, to)
				}
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}

			rows, err := historySymbolRows(cmd, db, sym, from, to, asOf)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				// Distinguish "symbol never synced" (typed not-found with a
				// sync hint) from "synced but no rows in this range" ([] +
				// coverage note — never fabricated rows).
				var total int
				var minD, maxD sql.NullString
				scanErr := db.DB().QueryRowContext(cmd.Context(),
					`SELECT COUNT(*), MIN(trading_date), MAX(trading_date) FROM pse_eod_prices WHERE symbol = ?`, sym,
				).Scan(&total, &minD, &maxD)
				if scanErr != nil && !syncHintMissingTable(scanErr) {
					return scanErr
				}
				if total == 0 {
					return notFoundErr(fmt.Errorf("no local data for symbol %q\nhint: run 'pse-edge-pp-cli sync market --symbols %s' to sync it first", sym, sym))
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "note: no local rows for %s in %s..%s (local coverage: %s..%s, %d rows). Not fabricating data.\n",
					sym, from, to, minD.String, maxD.String, total)
			}
			return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
		},
	}
	cmd.Flags().StringVar(&flagSince, "since", "30d", "Window ending at the last completed trading day (e.g. 30d, 12w)")
	cmd.Flags().StringVar(&flagFrom, "from", "", "Range start, YYYY-MM-DD (overrides --since)")
	cmd.Flags().StringVar(&flagTo, "to", "", "Range end, YYYY-MM-DD (default: last completed trading day)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path (default: resolved data directory data.db)")
	return cmd
}

// analyticsDateRange resolves the effective [from, to] date range from the
// optional --from/--to overrides, defaulting to the trailing window ending
// at the last completed trading day.
func analyticsDateRange(fromFlag, toFlag string, asOfDay time.Time, window time.Duration) (string, string, error) {
	to := asOfDay
	if toFlag != "" {
		t, err := time.ParseInLocation("2006-01-02", toFlag, psecal.Manila())
		if err != nil {
			return "", "", fmt.Errorf("invalid --to value %q: expected YYYY-MM-DD", toFlag)
		}
		to = t
	}
	from := to.Add(-window)
	if fromFlag != "" {
		f, err := time.ParseInLocation("2006-01-02", fromFlag, psecal.Manila())
		if err != nil {
			return "", "", fmt.Errorf("invalid --from value %q: expected YYYY-MM-DD", fromFlag)
		}
		from = f
	}
	if from.After(to) {
		return "", "", fmt.Errorf("--from %s is after --to %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
	return from.Format("2006-01-02"), to.Format("2006-01-02"), nil
}

// analyticsIsIndexCode reports whether sym is an index code with local
// snapshot rows (PSEI, sector indices). A missing table means no index
// data was ever synced — not an index, not an error.
func analyticsIsIndexCode(cmd *cobra.Command, db *store.Store, sym string) (bool, error) {
	var n int
	err := db.DB().QueryRowContext(cmd.Context(),
		`SELECT COUNT(*) FROM pse_index_snapshots WHERE index_code = ?`, sym).Scan(&n)
	if err != nil {
		if syncHintMissingTable(err) {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

// historySymbolRows reads pse_eod_prices bars in [from, to] ascending.
func historySymbolRows(cmd *cobra.Command, db *store.Store, sym, from, to, asOf string) ([]historyRow, error) {
	rows, err := db.DB().QueryContext(cmd.Context(),
		`SELECT trading_date, open, high, low, close, value, volume FROM pse_eod_prices
		 WHERE symbol = ? AND trading_date BETWEEN ? AND ? ORDER BY trading_date ASC`,
		sym, from, to)
	if err != nil {
		if syncHintMissingTable(err) {
			return []historyRow{}, nil
		}
		return nil, err
	}
	// Drain-first: fully consume and close the cursor before any further
	// queries on the same pooled connection budget.
	out := make([]historyRow, 0)
	for rows.Next() {
		var date string
		var open, high, low, closeP, value float64
		var volume sql.NullFloat64
		if err := rows.Scan(&date, &open, &high, &low, &closeP, &value, &volume); err != nil {
			rows.Close()
			return nil, err
		}
		r := historyRow{
			Date: date, Open: &open, High: &high, Low: &low,
			Close: closeP, Value: value,
			Source: "local", AsOf: asOf,
		}
		if volume.Valid {
			v := volume.Float64
			r.Volume = &v
		}
		r.VolumeStatus = volumeStatus(r.Volume)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	stale, err := analyticsSeriesStale(cmd, db, `SELECT MAX(trading_date) FROM pse_eod_prices WHERE symbol = ?`, sym, asOf)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Stale = stale
	}
	return out, nil
}

// historyIndexRows reads pse_index_snapshots readings in [from, to]
// ascending. Close mirrors value: the backfill series stores the daily
// close as value and has no OHLC.
func historyIndexRows(cmd *cobra.Command, db *store.Store, code, from, to, asOf string) ([]historyRow, error) {
	rows, err := db.DB().QueryContext(cmd.Context(),
		`SELECT trading_date, value FROM pse_index_snapshots
		 WHERE index_code = ? AND trading_date BETWEEN ? AND ? ORDER BY trading_date ASC`,
		code, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]historyRow, 0)
	for rows.Next() {
		var date string
		var value float64
		if err := rows.Scan(&date, &value); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, historyRow{
			Date: date, Close: value, Value: value,
			Source: "local", AsOf: asOf,
			VolumeStatus: volumeStatus(nil),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	stale, err := analyticsSeriesStale(cmd, db, `SELECT MAX(trading_date) FROM pse_index_snapshots WHERE index_code = ?`, code, asOf)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Stale = stale
	}
	return out, nil
}

// analyticsSeriesStale reports whether the newest stored trading date for
// the keyed series predates the last completed trading day.
func analyticsSeriesStale(cmd *cobra.Command, db *store.Store, query, key, lastCompleted string) (bool, error) {
	var maxDate sql.NullString
	err := db.DB().QueryRowContext(cmd.Context(), query, key).Scan(&maxDate)
	if err != nil {
		if syncHintMissingTable(err) {
			return true, nil
		}
		return false, err
	}
	if !maxDate.Valid {
		return true, nil
	}
	return maxDate.String < lastCompleted, nil
}

// historyEmptyNote emits a stderr coverage note for an empty index-range
// result so the caller can distinguish "no data in range" from "no data".
func historyEmptyNote(cmd *cobra.Command, db *store.Store, coverageQuery, key, from, to string) {
	var minD, maxD sql.NullString
	var total int
	if err := db.DB().QueryRowContext(cmd.Context(), coverageQuery, key).Scan(&minD, &maxD, &total); err != nil {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "note: no local rows for %s in %s..%s (local coverage: %s..%s, %d rows). Not fabricating data.\n",
		key, from, to, minD.String, maxD.String, total)
}
