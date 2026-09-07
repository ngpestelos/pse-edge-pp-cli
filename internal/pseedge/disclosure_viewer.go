// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
//
// Direct openDiscViewer.do lookup. PSE EDGE announcements/search.ax is not a
// complete corpus: filings can be openable by edge_no while never appearing in
// search results (repro: LODE 17-Q edge_no 2bc053ab3b1339fb…, filed 2026-07-22).

package pseedge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ph-commons/pse-edge-pp-cli/internal/cliutil"
)

// DisclosureViewerURL is the human-facing filing shell (same path as ViewerURL).
const DisclosureViewerURL = "https://edge.pse.com.ph/openDiscViewer.do"

const disclosureViewerMaxBody = 2 << 20

// DisclosureAttachment is one selectable attachment on the viewer shell.
type DisclosureAttachment struct {
	FileID string `json:"file_id"`
	Label  string `json:"label"`
}

// DisclosureViewer is metadata parsed from openDiscViewer.do HTML.
// It is the authoritative path when a filing is known by edge_no but missing
// from announcements/search.ax.
type DisclosureViewer struct {
	EdgeNo         string                 `json:"edge_no"`
	Company        string                 `json:"company"`
	Title          string                 `json:"title"`
	DisclosureDate string                 `json:"disclosure_date"` // YYYY-MM-DD when parseable
	RawDate        string                 `json:"raw_date,omitempty"`
	Attachments    []DisclosureAttachment `json:"attachments,omitempty"`
	DocumentFileID string                 `json:"document_file_id,omitempty"` // iframe downloadHtml.do
	ViewerURL      string                 `json:"viewer_url"`
	Source         string                 `json:"source"`
}

var (
	viewerCompanyRE    = regexp.MustCompile(`(?s)<div id="viewHeader">\s*<h2>([^<]*)</h2>`)
	viewerDateRE       = regexp.MustCompile(`(?s)Disclosure Date\s*:\s*([^<]+)</p>`)
	viewerTitleRE      = regexp.MustCompile(`(?s)<select id="docList"[^>]*>.*?<option[^>]*selected[^>]*>\s*([^<]*?)</option>`)
	viewerTitleLooseRE = regexp.MustCompile(`(?s)<select id="docList"[^>]*>.*?<option[^>]*>\s*([^<]*?)</option>`)
	viewerAttachRE     = regexp.MustCompile(`(?s)<option value="(\d+)">\s*([^<]*?)</option>`)
	viewerIFrameRE     = regexp.MustCompile(`downloadHtml\.do\?file_id=(\d+)`)
	viewerPageTitleRE  = regexp.MustCompile(`(?s)<title>([^<]*)</title>`)
)

// ParseDisclosureViewer parses openDiscViewer.do HTML for one edge_no shell.
func ParseDisclosureViewer(edgeNo, htmlBody string) (*DisclosureViewer, error) {
	if err := detectChallenge("openDiscViewer.do", htmlBody); err != nil {
		return nil, err
	}
	if !strings.Contains(htmlBody, `id="viewHeader"`) {
		return nil, &ShellPageError{Endpoint: "openDiscViewer.do", Reason: "viewHeader absent"}
	}
	v := &DisclosureViewer{
		EdgeNo:    strings.TrimSpace(edgeNo),
		ViewerURL: ViewerURL(edgeNo),
		Source:    "edge_viewer",
	}
	if m := viewerCompanyRE.FindStringSubmatch(htmlBody); len(m) == 2 {
		v.Company = cleanText(m[1])
	}
	if m := viewerDateRE.FindStringSubmatch(htmlBody); len(m) == 2 {
		v.RawDate = cleanText(m[1])
		v.DisclosureDate = parseViewerDate(v.RawDate)
	}
	titleRaw := ""
	if m := viewerTitleRE.FindStringSubmatch(htmlBody); len(m) == 2 {
		titleRaw = m[1]
	} else if m := viewerTitleLooseRE.FindStringSubmatch(htmlBody); len(m) == 2 {
		titleRaw = m[1]
	} else if m := viewerPageTitleRE.FindStringSubmatch(htmlBody); len(m) == 2 {
		titleRaw = m[1]
	}
	v.Title = normalizeViewerTitle(titleRaw)
	// Attachments: options under file_list with numeric values.
	if idx := strings.Index(htmlBody, `id="file_list"`); idx >= 0 {
		chunk := htmlBody[idx:]
		if end := strings.Index(chunk, `</select>`); end >= 0 {
			chunk = chunk[:end]
		}
		for _, m := range viewerAttachRE.FindAllStringSubmatch(chunk, -1) {
			v.Attachments = append(v.Attachments, DisclosureAttachment{
				FileID: cleanText(m[1]),
				Label:  cleanText(m[2]),
			})
		}
	}
	if m := viewerIFrameRE.FindStringSubmatch(htmlBody); len(m) == 2 {
		v.DocumentFileID = cleanText(m[1])
	}
	if v.Company == "" && v.Title == "" {
		return nil, &ShellPageError{Endpoint: "openDiscViewer.do", Reason: "no company/title parsed"}
	}
	return v, nil
}

func normalizeViewerTitle(s string) string {
	s = cleanText(s)
	// Option text is often "Jul 22, 2026  Quarterly Report"
	if i := strings.Index(s, "\u00a0"); i >= 0 {
		s = strings.TrimSpace(s[i+len("\u00a0"):])
	}
	// Fall back: drop a leading "Mon D, YYYY" prefix when present.
	parts := strings.Fields(s)
	if len(parts) >= 4 {
		// "Jul 22, 2026 Quarterly Report"
		if _, err := time.Parse("Jan 2, 2006", parts[0]+" "+parts[1]+" "+parts[2]); err == nil {
			s = strings.Join(parts[3:], " ")
		}
	}
	return strings.TrimSpace(s)
}

func parseViewerDate(raw string) string {
	raw = cleanText(raw)
	for _, layout := range []string{"Jan 2, 2006", "January 2, 2006"} {
		if t, err := time.ParseInLocation(layout, raw, manilaTZ); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// FetchDisclosureViewer GETs openDiscViewer.do?edge_no=… and parses the shell.
func FetchDisclosureViewer(ctx context.Context, hc *http.Client, edgeNo string) (*DisclosureViewer, error) {
	edgeNo = strings.TrimSpace(edgeNo)
	if edgeNo == "" {
		return nil, fmt.Errorf("pse-edge openDiscViewer.do: empty edge_no")
	}
	// Viewer hashes are lowercase hex (32+ chars). Reject obvious junk early
	// so we never send path-like or URL-like values as query params.
	for _, r := range edgeNo {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok {
			return nil, fmt.Errorf("pse-edge openDiscViewer.do: invalid edge_no %q (expected hex hash)", edgeNo)
		}
	}
	if len(edgeNo) < 16 {
		return nil, fmt.Errorf("pse-edge openDiscViewer.do: invalid edge_no %q (too short)", edgeNo)
	}
	endpoint := edgeOrigin() + "/openDiscViewer.do"
	u := endpoint + "?edge_no=" + url.QueryEscape(edgeNo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("pse-edge openDiscViewer.do: building request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	if hc == nil {
		hc = http.DefaultClient
	}
	disclosureLimiter.Wait()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pse-edge openDiscViewer.do: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, disclosureViewerMaxBody))
	if err != nil {
		return nil, fmt.Errorf("pse-edge openDiscViewer.do: reading response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		disclosureLimiter.OnRateLimit()
		retryAfter, _ := time.ParseDuration(strings.TrimSpace(resp.Header.Get("Retry-After")) + "s")
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, &cliutil.RateLimitError{URL: endpoint, RetryAfter: retryAfter, Body: preview}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pse-edge openDiscViewer.do: HTTP %d", resp.StatusCode)
	}
	disclosureLimiter.OnSuccess()
	return ParseDisclosureViewer(edgeNo, string(body))
}
