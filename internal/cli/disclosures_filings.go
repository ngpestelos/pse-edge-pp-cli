// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
//
// Hand-authored porcelain: disclosure search by ticker with local
// pse_disclosures index, plus direct edge_no viewer lookup.
//
// Naming note: the top-level name "disclosures" is already claimed by the
// generated resource command group (search/view/document), so per the
// build brief the porcelain is named "filings". The generated group stays
// untouched.
//
// pp:data-source live

package cli

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ph-commons/pse-edge-pp-cli/internal/psecal"
	"github.com/ph-commons/pse-edge-pp-cli/internal/pseedge"
	"github.com/ph-commons/pse-edge-pp-cli/internal/store"
)

// filingRow is one disclosure header row in porcelain output.
type filingRow struct {
	EdgeNo      string `json:"edge_no"`
	Symbol      string `json:"symbol,omitempty"`
	Company     string `json:"company"`
	Template    string `json:"template"`
	Title       string `json:"title"`
	DisclosedAt string `json:"disclosed_at"`
	ViewerURL   string `json:"viewer_url"`
	AsOf        string `json:"as_of"`
	Stale       bool   `json:"stale"`
	Source      string `json:"source"`
}

// filingsOut is the porcelain envelope: matched rows plus scan telemetry.
//
// Complete is true only relative to the announcements/search.ax result set
// (all pages scanned and all matched rows returned under --limit). It is
// NEVER a claim that PSE EDGE has published every filing into search —
// use filings get --edge-no for known disclosures missing from search.
type filingsOut struct {
	Corpus            string      `json:"corpus"`
	Rows              []filingRow `json:"rows"`
	ReturnedCount     int         `json:"returned_count"`
	ScannedPages      int         `json:"scanned_pages"`
	TotalPages        int         `json:"total_pages"`
	TotalCount        int         `json:"total_count"`
	FromDate          string      `json:"from_date"`
	ToDate            string      `json:"to_date"`
	CompanyID         string      `json:"company_id,omitempty"`
	Symbol            string      `json:"symbol,omitempty"`
	Limit             int         `json:"limit"`
	MaxScanPages      int         `json:"max_scan_pages"`
	Truncated         bool        `json:"truncated"`
	PageCapHit        bool        `json:"page_cap_hit"`
	Complete          bool        `json:"complete"`
	NewestDisclosedAt string      `json:"newest_disclosed_at,omitempty"`
	OldestDisclosedAt string      `json:"oldest_disclosed_at,omitempty"`
	FreshnessGapDays  *int        `json:"freshness_gap_days,omitempty"`
	Warnings          []string    `json:"warnings"`
	Note              string      `json:"note,omitempty"`
}

// Standing warning attached to every filings search response (issue #10).
const filingsSearchCorpusWarning = "announcements/search.ax is not an authoritative complete corpus: filings can exist on openDiscViewer.do that never appear in search results. For a known edge_no use structured lookup: pse-edge-pp-cli filings get --edge-no <hash> --json (disclosures view is raw HTML shell, not the same contract)."

// freshnessGapWarnDays: if the newest hit is this many calendar days before
// --to-date, surface a freshness-gap warning (upstream lag / missing rows).
const freshnessGapWarnDays = 7

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		root.AddCommand(newFilingsCmd(flags))
	})
}

func newFilingsCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	var templateFlag string
	var keywordFlag string
	var fromDateFlag string
	var toDateFlag string
	var limitFlag int
	var maxScanPages int
	var allCompanies bool

	cmd := &cobra.Command{
		Use:   "filings [symbol]",
		Short: "Search PSE Edge disclosures by ticker (or --all), upserting headers into the local pse_disclosures index",
		Long: `Use this command to list a company's disclosures (17-Q/17-A, dividend
declarations, material information, ...) by ticker symbol, or market-wide
with --all. Search is index-only: rows carry edge_no, not file_id.

Do NOT use search for computed filing-deadline status (use 'deadlines') or
to read a filing body. The one-shot for newest file_id + document body is:

  pse-edge-pp-cli filings latest-body SYMBOL --json

The symbol resolves to companyId via the local registry (live autocomplete
fallback); the search POSTs announcements/search.ax (form-urlencoded) and
pages through results. --template filters server-side (exact template name,
e.g. "Declaration of Cash Dividends"); --keyword is matched client-side on
titles because the endpoint ignores its keyword parameter (verified live)
— pages are scanned up to --max-scan-pages, and a zero-match scan that hits
the cap says so in the output note instead of pretending the corpus is empty.

Search is not the disclosure corpus: announcements/search.ax can omit
filings that remain available on openDiscViewer.do. Every search response
includes corpus=announcements_search_only and non-empty warnings, even when
complete=true. complete describes only the search result set; an empty
search does not prove that no filing exists.

Each omitted date defaults independently: --from-date is Manila today minus
90 calendar days; --to-date is Manila today. Dates use MM-DD-YYYY.
By default, scan at most 3 pages of 50 rows (up to 150 inspected) and return
at most 20 matches. Stop at --limit, the last page, or --max-scan-pages.
total_count is before the client-side --keyword filter.
A newest hit at least 7 calendar days before --to-date adds a freshness warning.

For a known edge_no missing from search:

  pse-edge-pp-cli filings get --edge-no <hash> --json

Details: https://github.com/ph-commons/pse-edge-pp-cli#search-is-not-the-disclosure-corpus

Every fetched header is upserted into the local pse_disclosures table for
offline joins (the 'deadlines' command reads it).`,
		Example: `  pse-edge-pp-cli filings GTCAP --from-date 01-01-2026 --json
  pse-edge-pp-cli filings GTCAP --template "Declaration of Cash Dividends" --json
  pse-edge-pp-cli filings --all --limit 50 --json
  pse-edge-pp-cli filings get --edge-no 2bc053ab3b1339fb64d70b69f0a3140b --json
  pse-edge-pp-cli filings latest-body GTCAP --json`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !allCompanies {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("filings takes at most one symbol, got %d", len(args)))
			}
			if len(args) == 1 && allCompanies {
				return usageErr(fmt.Errorf("pass either a symbol or --all, not both"))
			}
			if limitFlag <= 0 {
				return usageErr(fmt.Errorf("invalid --limit %d: must be positive", limitFlag))
			}
			if maxScanPages <= 0 {
				return usageErr(fmt.Errorf("invalid --max-scan-pages %d: must be positive", maxScanPages))
			}

			// Date window: MM-DD-YYYY, defaulting to trailing 90 days.
			now := time.Now().In(psecal.Manila())
			if fromDateFlag == "" {
				fromDateFlag = now.AddDate(0, 0, -90).Format("01-02-2006")
			}
			if toDateFlag == "" {
				toDateFlag = now.Format("01-02-2006")
			}
			for _, d := range []struct{ flag, val string }{{"--from-date", fromDateFlag}, {"--to-date", toDateFlag}} {
				if _, err := time.Parse("01-02-2006", d.val); err != nil {
					return usageErr(fmt.Errorf("invalid %s %q: expected MM-DD-YYYY", d.flag, d.val))
				}
			}

			if dbPath == "" {
				dbPath = defaultDBPath("pse-edge-pp-cli")
			}

			search := pseedge.DisclosureSearch{
				Template: templateFlag,
				FromDate: fromDateFlag,
				ToDate:   toDateFlag,
			}
			var reqSymbol string
			if !allCompanies {
				rc, err := resolvePSECompany(cmd.Context(), cmd, flags, dbPath, args[0])
				if err != nil {
					return err
				}
				reqSymbol = rc.Symbol
				search.CompanyID = strconv.Itoa(rc.CmpyID)
			}

			keyword := strings.ToLower(strings.TrimSpace(keywordFlag))
			// Explicit timeout: boundCtx cancels the request, but a bare
			// Client without Timeout can still strand idle connections if
			// the caller ever omits a deadline (issue #13).
			hc := &http.Client{Timeout: 60 * time.Second}
			out := filingsOut{
				Rows:         make([]filingRow, 0, limitFlag),
				FromDate:     fromDateFlag,
				ToDate:       toDateFlag,
				CompanyID:    search.CompanyID,
				Symbol:       reqSymbol,
				Limit:        limitFlag,
				MaxScanPages: maxScanPages,
			}
			lastCompleted := psecal.LastCompletedTradingDay(time.Now()).Format("2006-01-02")

			var fetched []pseedge.Disclosure
			for pageNo := 1; ; pageNo++ {
				reqCtx, cancel := boundCtx(cmd.Context(), flags)
				page, err := pseedge.FetchDisclosurePage(reqCtx, hc, search, pageNo)
				cancel()
				if err != nil {
					return classifyAPIError(err, flags)
				}
				out.ScannedPages++
				out.TotalPages = page.TotalPages
				out.TotalCount = page.TotalCount
				fetched = append(fetched, page.Rows...)

				for _, d := range page.Rows {
					if len(out.Rows) >= limitFlag {
						break
					}
					if keyword != "" && !strings.Contains(strings.ToLower(d.Title), keyword) {
						continue
					}
					// DisclosedAt is RFC3339 or "" (never the page string),
					// but guard the slice's shape anyway before date-slicing.
					asOf := lastCompleted
					if len(d.DisclosedAt) >= 10 {
						if _, err := time.Parse("2006-01-02", d.DisclosedAt[:10]); err == nil {
							asOf = d.DisclosedAt[:10]
						}
					}
					out.Rows = append(out.Rows, filingRow{
						EdgeNo:      d.EdgeNo,
						Symbol:      reqSymbol,
						Company:     d.Company,
						Template:    d.Template,
						Title:       d.Title,
						DisclosedAt: d.DisclosedAt,
						ViewerURL:   pseedge.ViewerURL(d.EdgeNo),
						AsOf:        asOf,
						Stale:       false,
						Source:      "edge",
					})
				}
				if len(out.Rows) >= limitFlag || pageNo >= page.TotalPages || out.ScannedPages >= maxScanPages {
					break
				}
			}

			finalizeFilingsOut(&out, keyword != "")

			// Persist every fetched header (matched or not) to the local
			// index. Best-effort: an index write failure warns, it never
			// blocks the live answer.
			if err := upsertFilings(cmd, dbPath, reqSymbol, fetched); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update local pse_disclosures index: %v\n", err)
			}

			// --all rows: annotate symbols from the local registry when known.
			if allCompanies {
				annotateFilingSymbols(cmd, dbPath, out.Rows, fetched)
			}

			// Mirror standing / truncation warnings on stderr for human runs.
			for _, w := range out.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			if out.Note != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "note: %s\n", out.Note)
			}

			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().StringVar(&templateFlag, "template", "", `Server-side disclosure template filter, exact name (e.g. "Declaration of Cash Dividends")`)
	cmd.Flags().StringVar(&keywordFlag, "keyword", "", "Client-side title keyword filter (the endpoint ignores its own keyword parameter)")
	cmd.Flags().StringVar(&fromDateFlag, "from-date", "", "Range start, MM-DD-YYYY (default: 90 days ago)")
	cmd.Flags().StringVar(&toDateFlag, "to-date", "", "Range end, MM-DD-YYYY (default: today, Manila)")
	cmd.Flags().IntVar(&limitFlag, "limit", 20, "Maximum rows to return")
	cmd.Flags().IntVar(&maxScanPages, "max-scan-pages", 3, "Maximum result pages to scan (50 rows/page)")
	cmd.Flags().BoolVar(&allCompanies, "all", false, "Search across all companies (no symbol filter)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path (default: resolved data directory data.db)")

	cmd.AddCommand(newFilingsGetCmd(flags))
	cmd.AddCommand(newFilingsLatestBodyCmd(flags))
	return cmd
}

// finalizeFilingsOut fills completeness telemetry and warnings. Pure helper
// for unit tests (issue #10).
func finalizeFilingsOut(out *filingsOut, keywordFilter bool) {
	out.Corpus = "announcements_search_only"
	out.ReturnedCount = len(out.Rows)
	out.PageCapHit = out.TotalPages > 0 && out.ScannedPages < out.TotalPages
	// Truncated: caller hit --limit while more search rows exist, or page cap
	// left pages unread.
	out.Truncated = out.PageCapHit || (out.Limit > 0 && out.ReturnedCount >= out.Limit && out.TotalCount > out.ReturnedCount)
	// Complete relative to the search result set only (not the universe of
	// filings). Client-side keyword filtering cannot claim completeness unless
	// every search page was scanned (matches may live on unscanned pages).
	out.Complete = !out.PageCapHit && !out.Truncated && out.ScannedPages > 0
	if keywordFilter && out.PageCapHit {
		out.Complete = false
		out.Truncated = true
	}

	// Newest/oldest from returned rows (what the agent sees).
	for _, r := range out.Rows {
		if len(r.DisclosedAt) < 10 {
			continue
		}
		day := r.DisclosedAt[:10]
		if out.NewestDisclosedAt == "" || day > out.NewestDisclosedAt {
			out.NewestDisclosedAt = day
		}
		if out.OldestDisclosedAt == "" || day < out.OldestDisclosedAt {
			out.OldestDisclosedAt = day
		}
	}

	out.Warnings = append([]string{}, filingsSearchCorpusWarning)

	// Freshness gap: newest hit vs --to-date.
	if out.NewestDisclosedAt != "" && out.ToDate != "" {
		if newest, err1 := time.Parse("2006-01-02", out.NewestDisclosedAt); err1 == nil {
			if toD, err2 := time.Parse("01-02-2006", out.ToDate); err2 == nil {
				gap := int(toD.Sub(newest).Hours() / 24)
				if gap < 0 {
					gap = 0
				}
				out.FreshnessGapDays = &gap
				if gap >= freshnessGapWarnDays && out.ReturnedCount > 0 {
					out.Warnings = append(out.Warnings, fmt.Sprintf(
						"newest search hit is %d calendar days before --to-date %s (newest=%s); upstream search lag or omitted filings are possible — do not treat this list as complete through --to-date",
						gap, out.ToDate, out.NewestDisclosedAt))
				}
			}
		}
	}

	switch {
	case out.ReturnedCount == 0 && out.PageCapHit:
		out.Note = fmt.Sprintf("no matches in the %d of %d result pages scanned (--max-scan-pages cap); raise --max-scan-pages or narrow the date window", out.ScannedPages, out.TotalPages)
		out.Complete = false
	case out.ReturnedCount == 0:
		out.Note = "no disclosures matched filters in the full result set (search index only — not proof that no filing exists on the official viewer)"
		// full scan of empty/non-matching set
		out.Complete = !out.PageCapHit && out.ScannedPages > 0
	case out.Truncated && out.ReturnedCount >= out.Limit:
		if keywordFilter {
			out.Note = fmt.Sprintf("truncated at --limit %d after scanning search pages (search reports %d total rows before client-side keyword filter)", out.Limit, out.TotalCount)
		} else {
			out.Note = fmt.Sprintf("truncated at --limit %d of %d total disclosures reported by search", out.Limit, out.TotalCount)
		}
		out.Complete = false
	case out.PageCapHit:
		out.Note = fmt.Sprintf("page cap: scanned %d of %d result pages (--max-scan-pages); raise the cap or narrow the window", out.ScannedPages, out.TotalPages)
		out.Complete = false
	default:
		if out.Note == "" {
			out.Note = fmt.Sprintf("search returned %d row(s); complete=%v relative to announcements/search.ax only", out.ReturnedCount, out.Complete)
		}
	}
	if out.ReturnedCount == 0 {
		out.Note += "; for a known edge_no use pse-edge-pp-cli filings get --edge-no <hash> --json"
	}
}

func newFilingsGetCmd(flags *rootFlags) *cobra.Command {
	var edgeNo string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Fetch one filing shell by edge_no via openDiscViewer.do (authoritative when search omits it)",
		Long: `Direct lookup of a disclosure by edge_no on openDiscViewer.do.

Use this when announcements/search.ax (the filings search path) omits a
filing that is still openable on the official PSE EDGE viewer. Example
from issue #10: LODE SEC Form 17-Q filed 2026-07-22 was missing from
search but present at:

  https://edge.pse.com.ph/openDiscViewer.do?edge_no=2bc053ab3b1339fb64d70b69f0a3140b

This command does not depend on the search index. For full document HTML
use 'disclosures document' / downloadHtml.do after reading attachment file_ids.`,
		Example:     `  pse-edge-pp-cli filings get --edge-no 2bc053ab3b1339fb64d70b69f0a3140b --json`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			edgeNo = strings.TrimSpace(edgeNo)
			if edgeNo == "" && len(args) == 1 {
				edgeNo = strings.TrimSpace(args[0])
			}
			if edgeNo == "" {
				if flags.asJSON {
					_ = printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"error": "requires --edge-no",
						"usage": cmd.CommandPath() + " --edge-no <hash>",
					}, flags)
					return usageErr(fmt.Errorf("%q requires --edge-no", cmd.CommandPath()))
				}
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			reqCtx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			v, err := pseedge.FetchDisclosureViewer(reqCtx, &http.Client{Timeout: 60 * time.Second}, edgeNo)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			out := map[string]any{
				"edge_no":          v.EdgeNo,
				"company":          v.Company,
				"title":            v.Title,
				"disclosure_date":  v.DisclosureDate,
				"raw_date":         v.RawDate,
				"attachments":      v.Attachments,
				"document_file_id": v.DocumentFileID,
				"viewer_url":       v.ViewerURL,
				"source":           v.Source,
				"as_of":            v.DisclosureDate,
				"stale":            false,
				"note":             "direct openDiscViewer.do lookup; independent of announcements/search.ax",
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().StringVar(&edgeNo, "edge-no", "", "Disclosure edge_no hash from a viewer URL or search row")
	return cmd
}

// upsertFilings writes fetched disclosure headers into pse_disclosures.
// symbol may be "" (market-wide scan); cmpy_id is always recorded so a
// later registry join can backfill symbols.
func upsertFilings(cmd *cobra.Command, dbPath, symbol string, rows []pseedge.Disclosure) error {
	if len(rows) == 0 {
		return nil
	}
	db, err := store.OpenWithContext(cmd.Context(), dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.EnsurePSEEdgeTables(cmd.Context()); err != nil {
		return err
	}
	storeRows := make([]store.PSEDisclosureRow, 0, len(rows))
	for _, d := range rows {
		sym := symbol
		if sym == "" {
			if co, err := db.LookupPSECompanyByCmpyID(cmd.Context(), d.CmpyID); err == nil {
				sym = co.Symbol
			}
		}
		storeRows = append(storeRows, store.PSEDisclosureRow{
			EdgeNo:      d.EdgeNo,
			CmpyID:      d.CmpyID,
			Symbol:      sym,
			Template:    d.Template,
			Title:       d.Title,
			DisclosedAt: d.DisclosedAt,
		})
	}
	return db.UpsertPSEDisclosures(cmd.Context(), storeRows)
}

// annotateFilingSymbols backfills the symbol field on --all output rows
// from the local registry (best-effort; unknown issuers stay blank).
func annotateFilingSymbols(cmd *cobra.Command, dbPath string, rows []filingRow, fetched []pseedge.Disclosure) {
	db, err := store.OpenReadOnlyContext(cmd.Context(), dbPath)
	if err != nil {
		return
	}
	defer db.Close()
	byEdgeNo := make(map[string]int, len(fetched))
	for _, d := range fetched {
		byEdgeNo[d.EdgeNo] = d.CmpyID
	}
	cache := map[int]string{}
	for i := range rows {
		cmpyID, ok := byEdgeNo[rows[i].EdgeNo]
		if !ok {
			continue
		}
		sym, cached := cache[cmpyID]
		if !cached {
			if co, err := db.LookupPSECompanyByCmpyID(cmd.Context(), cmpyID); err == nil {
				sym = co.Symbol
			}
			cache[cmpyID] = sym
		}
		rows[i].Symbol = sym
	}
}
