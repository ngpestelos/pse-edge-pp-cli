// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ph-commons/pse-edge-pp-cli/internal/pseedge"
	"github.com/ph-commons/pse-edge-pp-cli/internal/store"
)

func TestPickLatestBodyFileIDPrefersDocumentFileID(t *testing.T) {
	v := &pseedge.DisclosureViewer{
		DocumentFileID: "1946761",
		Attachments: []pseedge.DisclosureAttachment{
			{FileID: "1946762", Label: "attachment"},
		},
	}
	got, err := pickLatestBodyFileID(v)
	if err != nil {
		t.Fatalf("pickLatestBodyFileID: %v", err)
	}
	if got != "1946761" {
		t.Fatalf("file_id = %q, want 1946761", got)
	}
}

func TestPickLatestBodyFileIDFallsBackToAttachment(t *testing.T) {
	v := &pseedge.DisclosureViewer{
		Attachments: []pseedge.DisclosureAttachment{
			{FileID: "1946762", Label: "attachment"},
		},
	}
	got, err := pickLatestBodyFileID(v)
	if err != nil {
		t.Fatalf("pickLatestBodyFileID: %v", err)
	}
	if got != "1946762" {
		t.Fatalf("file_id = %q, want 1946762", got)
	}
}

func TestPickLatestBodyFileIDEmptyErrors(t *testing.T) {
	_, err := pickLatestBodyFileID(&pseedge.DisclosureViewer{EdgeNo: "abc"})
	if err == nil {
		t.Fatal("expected error for empty viewer file ids")
	}
	if code := ExitCode(err); code == 0 {
		t.Fatalf("ExitCode(%v) = 0, want non-zero", err)
	}
}

func TestAssembleLatestFilingBody(t *testing.T) {
	html := []byte(`<!doctype html><html><body><p>Quarterly report body</p></body></html>`)
	raw, err := decodeDisclosureDocument(context.Background(), "1946761", html)
	if err != nil {
		t.Fatalf("decodeDisclosureDocument: %v", err)
	}
	v := &pseedge.DisclosureViewer{
		EdgeNo:         "2bc053ab3b1339fb64d70b69f0a3140b",
		DocumentFileID: "1946761",
		Attachments: []pseedge.DisclosureAttachment{
			{FileID: "1946762", Label: "attachment"},
		},
	}
	out, err := assembleLatestFilingBody(v, "1946761", raw)
	if err != nil {
		t.Fatalf("assembleLatestFilingBody: %v", err)
	}
	if out.EdgeNo != v.EdgeNo {
		t.Errorf("edge_no = %q", out.EdgeNo)
	}
	if out.FileID != "1946761" {
		t.Errorf("file_id = %q, want 1946761", out.FileID)
	}
	if out.DocumentFileID != "1946761" {
		t.Errorf("document_file_id = %q, want 1946761", out.DocumentFileID)
	}
	if len(out.Attachments) != 1 || out.Attachments[0].FileID != "1946762" {
		t.Errorf("attachments = %+v", out.Attachments)
	}
	if out.ContentType != "text/html" {
		t.Errorf("content_type = %q", out.ContentType)
	}
	if !strings.Contains(out.Text, "Quarterly report body") {
		t.Errorf("text %q missing body", out.Text)
	}
	if out.ByteLength != len(html) {
		t.Errorf("byte_length = %d, want %d", out.ByteLength, len(html))
	}
}

func TestFilingSearchRowOmitsFileID(t *testing.T) {
	raw, err := json.Marshal(filingRow{EdgeNo: "abc", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("file_id")) {
		t.Fatalf("search row must omit file_id: %s", raw)
	}
}

func TestFilingsLatestBodyHelpWired(t *testing.T) {
	cmd := RootCmd()
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"filings", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("filings --help: %v", err)
	}
	help := buf.String()
	for _, want := range []string{"latest-body", "index-only", "one-shot"} {
		if !strings.Contains(help, want) {
			t.Fatalf("filings help missing %q:\n%s", want, help)
		}
	}
}

func TestFilingsLatestBodyJSONFileIDAndBody(t *testing.T) {
	for _, format := range []string{"text/html", "application/pdf"} {
		t.Run(format, func(t *testing.T) {
			isolateDocumentCmdHome(t)
			dbPath := seedGTCAPTestDB(t)
			searchHTML := readCLIFixture(t, "disclosures_search_gtcap.html")
			viewerHTML := readCLIFixture(t, "disclosure_viewer_lode_17q.html")
			docHTML := []byte(`<!doctype html><html><body><p>Latest filing body text</p></body></html>`)

			if format == "application/pdf" {
				requirePDFToText(t)
				docHTML = disclosurePDF("Latest filing body text")
			}
			var gotDownloadID string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/announcements/search.ax":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write(searchHTML)
				case r.Method == http.MethodGet && r.URL.Path == "/openDiscViewer.do":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write(viewerHTML)
				case r.Method == http.MethodGet && r.URL.Path == "/downloadHtml.do":
					gotDownloadID = r.URL.Query().Get("file_id")
					w.Header().Set("Content-Type", format)
					_, _ = w.Write(docHTML)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv("PSE_EDGE_BASE_URL", srv.URL)

			rootCmd := RootCmd()
			var stdout, stderr bytes.Buffer
			rootCmd.SetOut(&stdout)
			rootCmd.SetErr(&stderr)
			rootCmd.SetArgs([]string{
				"filings", "latest-body", "GTCAP",
				"--db", dbPath,
				"--from-date", "01-01-2026",
				"--to-date", "07-27-2026",
				"--json", "--no-learn", "--no-cache",
			})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("filings latest-body: %v (stderr=%q stdout=%q)", err, stderr.String(), stdout.String())
			}
			if gotDownloadID != "1946761" {
				t.Fatalf("downloadHtml file_id = %q, want 1946761", gotDownloadID)
			}
			trimmed := bytes.TrimSpace(stdout.Bytes())
			if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
				t.Fatalf("stdout is empty object: %q", stdout.String())
			}
			var out latestFilingBody
			if err := json.Unmarshal(trimmed, &out); err != nil {
				t.Fatalf("stdout JSON: %v (stdout=%q)", err, stdout.String())
			}
			if out.FileID != "1946761" {
				t.Errorf("file_id = %q, want 1946761", out.FileID)
			}
			if out.EdgeNo != "83eed7f77a89ed3964d70b69f0a3140b" {
				t.Errorf("edge_no = %q, want first search row", out.EdgeNo)
			}
			if out.ContentType != format {
				t.Errorf("content_type = %q, want %s", out.ContentType, format)
			}
			if !strings.Contains(out.Text, "Latest filing body text") {
				t.Errorf("text %q missing document body", out.Text)
			}
		})
	}
}

func TestFilingsLatestBodyEmptySearchNonZero(t *testing.T) {
	isolateDocumentCmdHome(t)
	dbPath := seedGTCAPTestDB(t)
	empty := []byte(`<span class="count">[1 / 1] [Total 0]</span>`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/announcements/search.ax" {
			_, _ = w.Write(empty)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PSE_EDGE_BASE_URL", srv.URL)

	rootCmd := RootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{
		"filings", "latest-body", "GTCAP",
		"--db", dbPath,
		"--from-date", "01-01-2026",
		"--to-date", "07-27-2026",
		"--json", "--no-learn", "--no-cache",
	})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit for empty search")
	}
	if code := ExitCode(err); code == 0 {
		t.Fatalf("ExitCode(%v) = 0, want non-zero", err)
	}
}

func TestFilingsLatestBodyEmptyDocumentNonZero(t *testing.T) {
	isolateDocumentCmdHome(t)
	dbPath := seedGTCAPTestDB(t)
	searchHTML := readCLIFixture(t, "disclosures_search_gtcap.html")
	viewerHTML := readCLIFixture(t, "disclosure_viewer_lode_17q.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/announcements/search.ax":
			_, _ = w.Write(searchHTML)
		case r.URL.Path == "/openDiscViewer.do":
			_, _ = w.Write(viewerHTML)
		case r.URL.Path == "/downloadHtml.do":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PSE_EDGE_BASE_URL", srv.URL)

	rootCmd := RootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{
		"filings", "latest-body", "GTCAP",
		"--db", dbPath,
		"--from-date", "01-01-2026",
		"--to-date", "07-27-2026",
		"--json", "--no-learn", "--no-cache",
	})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit for empty document")
	}
	if code := ExitCode(err); code == 0 {
		t.Fatalf("ExitCode(%v) = 0, want non-zero", err)
	}
}

func seedGTCAPTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.EnsurePSEEdgeTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}
	err = db.UpsertPSECompanies(context.Background(), []store.PSECompanyRow{{
		CmpyID:     633,
		SecurityID: 572,
		Symbol:     "GTCAP",
		Name:       "GT Capital Holdings, Inc.",
	}})
	if err != nil {
		t.Fatalf("upsert GTCAP: %v", err)
	}
	return dbPath
}

func readCLIFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pseedge", "testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return data
}
