// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type disclosureDocumentBody struct {
	FileID      string `json:"file_id"`
	ContentType string `json:"content_type"`
	Text        string `json:"text"`
	ByteLength  int    `json:"byte_length"`
}

func decodeDisclosureDocumentJSON(t *testing.T, raw json.RawMessage) disclosureDocumentBody {
	t.Helper()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		t.Fatalf("decodeDisclosureDocument returned empty object %q", raw)
	}
	var body disclosureDocumentBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decodeDisclosureDocument JSON: %v (raw=%s)", err, raw)
	}
	return body
}

func TestDecodeDisclosureDocumentHTMLVisibleText(t *testing.T) {
	html := []byte(`<!doctype html><html><head><title>Viewer</title></head><body><p>Board approved the buyback program.</p></body></html>`)
	raw, err := decodeDisclosureDocument("1948180", html)
	if err != nil {
		t.Fatalf("decodeDisclosureDocument: %v", err)
	}
	got := decodeDisclosureDocumentJSON(t, raw)
	if got.FileID != "1948180" {
		t.Errorf("file_id = %q, want 1948180", got.FileID)
	}
	if got.ContentType != "text/html" {
		t.Errorf("content_type = %q, want text/html", got.ContentType)
	}
	if got.ByteLength != len(html) {
		t.Errorf("byte_length = %d, want %d", got.ByteLength, len(html))
	}
	if !strings.Contains(got.Text, "Board approved the buyback program") {
		t.Errorf("text %q missing visible body", got.Text)
	}
}

func TestDecodeDisclosureDocumentPDFMagic(t *testing.T) {
	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")
	raw, err := decodeDisclosureDocument("1959980", pdf)
	if err != nil {
		t.Fatalf("decodeDisclosureDocument: %v", err)
	}
	got := decodeDisclosureDocumentJSON(t, raw)
	if got.FileID != "1959980" {
		t.Errorf("file_id = %q, want 1959980", got.FileID)
	}
	if got.ContentType != "application/pdf" {
		t.Errorf("content_type = %q, want application/pdf", got.ContentType)
	}
	if got.ByteLength != len(pdf) {
		t.Errorf("byte_length = %d, want %d", got.ByteLength, len(pdf))
	}
}

func TestDecodeDisclosureDocumentPDFBinaryEnvelope(t *testing.T) {
	pdf := []byte("%PDF-1.4\n%%EOF\n")
	envelope, err := json.Marshal(map[string]any{
		"_pp_binary":   true,
		"content_type": "application/pdf",
		"encoding":     "base64",
		"bytes":        len(pdf),
		"data":         base64.StdEncoding.EncodeToString(pdf),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeDisclosureDocument("1959980", envelope)
	if err != nil {
		t.Fatalf("decodeDisclosureDocument: %v", err)
	}
	got := decodeDisclosureDocumentJSON(t, raw)
	if got.ContentType != "application/pdf" {
		t.Errorf("content_type = %q, want application/pdf", got.ContentType)
	}
	if got.ByteLength != len(pdf) {
		t.Errorf("byte_length = %d, want %d", got.ByteLength, len(pdf))
	}
	if got.FileID != "1959980" {
		t.Errorf("file_id = %q, want 1959980", got.FileID)
	}
}

func TestDecodeDisclosureDocumentEmptyRaw(t *testing.T) {
	_, err := decodeDisclosureDocument("1959980", nil)
	if err == nil {
		t.Fatal("expected error for empty raw")
	}
	if code := ExitCode(err); code == 0 {
		t.Fatalf("ExitCode(%v) = 0, want non-zero", err)
	}

	_, err = decodeDisclosureDocument("1959980", []byte{})
	if err == nil {
		t.Fatal("expected error for zero-length raw")
	}
	if code := ExitCode(err); code == 0 {
		t.Fatalf("ExitCode(%v) = 0, want non-zero", err)
	}
}

func TestDecodeDisclosureDocumentViewerShellHTML(t *testing.T) {
	html := []byte(`<html><body><div id="viewer">Quarterly report notes without og tags.</div></body></html>`)
	raw, err := decodeDisclosureDocument("1888001", html)
	if err != nil {
		t.Fatalf("decodeDisclosureDocument: %v", err)
	}
	got := decodeDisclosureDocumentJSON(t, raw)
	if got.ContentType != "text/html" {
		t.Errorf("content_type = %q, want text/html", got.ContentType)
	}
	if !strings.Contains(got.Text, "Quarterly report notes without og tags") {
		t.Errorf("text %q missing viewer-shell body", got.Text)
	}
}

func isolateDocumentCmdHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PSE_EDGE_NO_LEARN", "true")
	t.Setenv("PSE_EDGE_STATE_DIR", "")
	t.Setenv("PSE_EDGE_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("PSE_EDGE_CONFIG", filepath.Join(home, "missing-config.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
}

func TestDisclosuresDocumentJSONPDFBody(t *testing.T) {
	isolateDocumentCmdHome(t)

	pdf := []byte("%PDF-1.4\n% known-good EDGE attachment bytes\n%%EOF\n")
	var gotFileID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/downloadHtml.do" {
			http.NotFound(w, r)
			return
		}
		gotFileID = r.URL.Query().Get("file_id")
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdf)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PSE_EDGE_BASE_URL", srv.URL)

	rootCmd := RootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"disclosures", "document", "--file-id", "1959980", "--json", "--no-learn", "--no-cache"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("disclosures document: %v (stderr=%q stdout=%q)", err, stderr.String(), stdout.String())
	}
	if gotFileID != "1959980" {
		t.Fatalf("GET file_id = %q, want 1959980", gotFileID)
	}

	var envelope struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout JSON: %v (stdout=%q)", err, stdout.String())
	}
	trimmed := bytes.TrimSpace(envelope.Results)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		t.Fatalf("results is empty: %s", stdout.String())
	}
	var body disclosureDocumentBody
	if err := json.Unmarshal(envelope.Results, &body); err != nil {
		t.Fatalf("results JSON: %v (results=%s)", err, envelope.Results)
	}
	if body.ContentType != "application/pdf" {
		t.Errorf("results.content_type = %q, want application/pdf", body.ContentType)
	}
	if body.FileID != "1959980" {
		t.Errorf("results.file_id = %q, want 1959980", body.FileID)
	}
}

func TestDisclosuresDocumentEmptyBodyNonZero(t *testing.T) {
	isolateDocumentCmdHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PSE_EDGE_BASE_URL", srv.URL)

	rootCmd := RootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"disclosures", "document", "--file-id", "1959980", "--json", "--no-learn", "--no-cache"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit for empty document body")
	}
	if code := ExitCode(err); code == 0 {
		t.Fatalf("ExitCode(%v) = 0, want non-zero", err)
	}

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "data.db")); err == nil {
		t.Fatal("test wrote data.db under isolated HOME")
	}
}
