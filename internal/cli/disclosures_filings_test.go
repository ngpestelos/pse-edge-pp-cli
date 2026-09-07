// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestFinalizeFilingsOutStandingWarning(t *testing.T) {
	out := filingsOut{
		Rows: []filingRow{
			{DisclosedAt: "2026-06-30T15:53:00+08:00"},
		},
		ScannedPages: 1,
		TotalPages:   1,
		TotalCount:   19,
		FromDate:     "01-01-2024",
		ToDate:       "08-05-2026",
		Limit:        20,
		MaxScanPages: 3,
	}
	finalizeFilingsOut(&out, false)
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "not an authoritative complete corpus") {
		t.Fatalf("missing corpus warning: %v", out.Warnings)
	}
	if out.NewestDisclosedAt != "2026-06-30" {
		t.Errorf("newest = %q", out.NewestDisclosedAt)
	}
	if out.FreshnessGapDays == nil || *out.FreshnessGapDays < freshnessGapWarnDays {
		t.Fatalf("freshness_gap_days = %v, want >= %d", out.FreshnessGapDays, freshnessGapWarnDays)
	}
	foundGap := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "newest search hit") {
			foundGap = true
		}
	}
	if !foundGap {
		t.Fatalf("expected freshness-gap warning, got %v", out.Warnings)
	}
	// LODE-shaped: full page scan of search set, under limit → complete relative to search
	if !out.Complete {
		t.Errorf("complete = false, want true relative to full search set; note=%q truncated=%v page_cap=%v", out.Note, out.Truncated, out.PageCapHit)
	}
	if out.ReturnedCount != 1 {
		t.Errorf("returned_count = %d", out.ReturnedCount)
	}
}

func TestFinalizeFilingsOutPageCap(t *testing.T) {
	out := filingsOut{
		Rows:         []filingRow{{DisclosedAt: "2026-01-01T00:00:00+08:00"}},
		ScannedPages: 3,
		TotalPages:   10,
		TotalCount:   500,
		FromDate:     "01-01-2024",
		ToDate:       "01-05-2024",
		Limit:        20,
		MaxScanPages: 3,
	}
	finalizeFilingsOut(&out, false)
	if out.Complete {
		t.Error("complete should be false when page cap hit")
	}
	if !out.PageCapHit || !out.Truncated {
		t.Errorf("page_cap_hit=%v truncated=%v", out.PageCapHit, out.Truncated)
	}
	if !strings.Contains(out.Note, "page cap") {
		t.Errorf("note = %q", out.Note)
	}
}

func TestFinalizeFilingsOutLimitTruncate(t *testing.T) {
	rows := make([]filingRow, 20)
	for i := range rows {
		rows[i] = filingRow{DisclosedAt: "2026-07-01T00:00:00+08:00"}
	}
	out := filingsOut{
		Rows:         rows,
		ScannedPages: 1,
		TotalPages:   1,
		TotalCount:   50,
		FromDate:     "01-01-2026",
		ToDate:       "07-02-2026",
		Limit:        20,
		MaxScanPages: 3,
	}
	finalizeFilingsOut(&out, false)
	if out.Complete || !out.Truncated {
		t.Errorf("complete=%v truncated=%v", out.Complete, out.Truncated)
	}
	if !strings.Contains(out.Note, "truncated at --limit") {
		t.Errorf("note = %q", out.Note)
	}
}

func TestFilingsGetHelpWired(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"filings", "get", "--help"})
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("filings get --help: %v", err)
	}
	help := buf.String()
	for _, want := range []string{"edge-no", "openDiscViewer"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
}

func TestFinalizeFilingsOutKeywordLimitNote(t *testing.T) {
	rows := make([]filingRow, 5)
	for i := range rows {
		rows[i] = filingRow{DisclosedAt: "2026-07-01T00:00:00+08:00"}
	}
	out := filingsOut{
		Rows:         rows,
		ScannedPages: 1,
		TotalPages:   1,
		TotalCount:   50,
		FromDate:     "01-01-2026",
		ToDate:       "07-02-2026",
		Limit:        5,
		MaxScanPages: 3,
	}
	finalizeFilingsOut(&out, true)
	if !strings.Contains(out.Note, "before client-side keyword filter") {
		t.Fatalf("note = %q", out.Note)
	}
}

func TestFilingsGetAgentKeepsAttachments(t *testing.T) {
	// compactObjectFields must not strip attachments (primary get payload).
	raw := []byte(`{"edge_no":"abc","attachments":[{"file_id":"1","label":"x"}],"document_file_id":"2","title":"t"}`)
	got := compactFields(raw)
	s := string(got)
	if !strings.Contains(s, "attachments") || !strings.Contains(s, "file_id") {
		t.Fatalf("compact stripped attachments: %s", s)
	}
	if !strings.Contains(s, "document_file_id") {
		t.Fatalf("compact stripped document_file_id: %s", s)
	}
}

func TestFilingsSearchAgentCorpus(t *testing.T) {
	for _, tc := range []struct {
		name               string
		rows, pages, total int
		complete           bool
	}{
		{"full", 1, 1, 1, true}, {"empty", 0, 1, 0, true},
		{"capped", 1, 5, 250, false}, {"empty capped", 0, 5, 250, false},
		{"limited", 20, 1, 50, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filingsOut{Rows: make([]filingRow, tc.rows), ScannedPages: 1, TotalPages: tc.pages, TotalCount: tc.total, Limit: 20, MaxScanPages: 1}
			finalizeFilingsOut(&out, false)
			var buf strings.Builder
			if err := printJSONFiltered(&buf, out, &rootFlags{agent: true, asJSON: true, compact: true}); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Corpus   string
				Warnings []string
				Complete bool
				Note     string
			}
			if err := json.Unmarshal([]byte(buf.String()), &struct {
				Results any `json:"results"`
			}{Results: &got}); err != nil {
				t.Fatal(err)
			}
			if got.Corpus != "announcements_search_only" {
				t.Errorf("corpus = %q", got.Corpus)
			}
			if len(got.Warnings) == 0 || !strings.Contains(got.Warnings[0], "not an authoritative complete corpus") {
				t.Errorf("warnings = %v", got.Warnings)
			}
			if got.Complete != tc.complete {
				t.Errorf("complete = %v, want %v", got.Complete, tc.complete)
			}
			if tc.rows == 0 && !strings.Contains(got.Note, "filings get --edge-no") {
				t.Errorf("empty recovery note = %q", got.Note)
			}
		})
	}
}

func TestFilingsSearchHelpCorpus(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"filings", "--help"})
	var buf strings.Builder
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#search-is-not-the-disclosure-corpus", "announcements_search_only", "90 calendar days", "150", "total_count", "filings get --edge-no"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("help missing %q", want)
		}
	}
}

type filingsTransport func(*http.Request) (*http.Response, error)

func (f filingsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFilingsGetIndependentOfSearch(t *testing.T) {
	fixture, err := os.ReadFile("../pseedge/testdata/disclosure_viewer_lode_17q.html")
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	requests := 0
	edge := "2bc053ab3b1339fb64d70b69f0a3140b"
	http.DefaultTransport = filingsTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != "GET" || r.URL.Path != "/openDiscViewer.do" || r.URL.Query().Get("edge_no") != edge {
			return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(fixture))), Request: r}, nil
	})
	cmd := RootCmd()
	cmd.SetArgs([]string{"filings", "get", "--edge-no", edge, "--agent", "--no-learn"})
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Title          string
		DocumentFileID string `json:"document_file_id"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &struct {
		Results any `json:"results"`
	}{Results: &got}); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || got.Title != "Quarterly Report" || got.DocumentFileID != "1946761" {
		t.Fatalf("requests=%d output=%s", requests, buf.String())
	}
}
