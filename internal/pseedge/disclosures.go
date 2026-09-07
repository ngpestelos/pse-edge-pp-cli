// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

// Disclosure-search parser and fetcher for POST /announcements/search.ax.
// Live-verified 2026-07-27:
//
//   - The endpoint requires an application/x-www-form-urlencoded body
//     (companyId=&keyword=&tmplNm=&fromDate=MM-DD-YYYY&toDate=MM-DD-YYYY&pageNo=N);
//     a JSON body is silently treated as an empty search, so this fetcher
//     does NOT ride the generated JSON client.
//   - tmplNm filters server-side; the keyword param is IGNORED server-side —
//     callers must scan-and-filter titles client-side.
//   - Pager: <span class="count">[PAGE / PAGES] [Total N]</span>, 50 rows/page.
//   - Row: company anchor (href carries cmpy_id), title anchor
//     (onclick openPopup('EDGE_NO')), template code cell, timestamp cell.

package pseedge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ph-commons/pse-edge-pp-cli/internal/cliutil"
)

// disclosureLimiter paces the form-POST fetcher (which bypasses the generated
// client and its limiter). One request/second start rate, adaptive ceiling.
var disclosureLimiter = cliutil.NewAdaptiveLimiterAuto(1)

// DisclosureSearchURL is the production announcements search endpoint
// (form POST, no auth). Fetchers honor PSE_EDGE_BASE_URL when set.
const DisclosureSearchURL = "https://edge.pse.com.ph/announcements/search.ax"

const productionEdgeOrigin = "https://edge.pse.com.ph"

// edgeOrigin is the EDGE host used by hand-built fetchers. Production
// default is unchanged; tests point httptest via PSE_EDGE_BASE_URL
// (trim trailing slash, then append the path).
func edgeOrigin() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("PSE_EDGE_BASE_URL")), "/"); v != "" {
		return v
	}
	return productionEdgeOrigin
}

func disclosureSearchEndpoint() string {
	return edgeOrigin() + "/announcements/search.ax"
}

// disclosureMaxBody caps how much of a response is read (pages are ~14KB;
// the cap only guards against a pathological upstream).
const disclosureMaxBody = 4 << 20

// Disclosure is one parsed search-result row.
type Disclosure struct {
	EdgeNo      string `json:"edge_no"`
	CmpyID      int    `json:"cmpy_id"`
	Company     string `json:"company"`
	Template    string `json:"template"` // form code as printed, e.g. "14-1", "17-Q"
	Title       string `json:"title"`
	DisclosedAt string `json:"disclosed_at"` // RFC3339 Asia/Manila; "" when the page timestamp is unparseable
	// RawTimestamp preserves the page's printed timestamp only when it
	// failed every known layout (DisclosedAt is then ""). The raw string is
	// never placed in DisclosedAt — date-slicing consumers must not see it.
	RawTimestamp string `json:"raw_timestamp,omitempty"`
	CircularNo   string `json:"circular_no,omitempty"`
}

// DisclosurePage is one parsed page of search results plus pager state.
type DisclosurePage struct {
	Rows       []Disclosure
	PageNo     int
	TotalPages int
	TotalCount int
}

// DisclosureSearch are the server-side search parameters. Keyword is
// deliberately absent: the server ignores it (verified live 2026-07-27).
type DisclosureSearch struct {
	CompanyID string // numeric cmpy_id, or "" for all companies
	Template  string // tmplNm, exact template name, or ""
	FromDate  string // MM-DD-YYYY
	ToDate    string // MM-DD-YYYY
}

var (
	disclosureRowRE = regexp.MustCompile(
		`(?s)<td><a href="/companyInformation/form\.do\?cmpy_id=(\d+)">([^<]*)</a></td>\s*` +
			`<td><a href="#viewer" onclick="openPopup\('([0-9a-f]+)'\);return false;">(.*?)</a></td>\s*` +
			`<td class="alignC">([^<]*)</td>\s*` +
			`<td class="alignC">([^<]*)</td>\s*` +
			`<td class="alignC">([^<]*)</td>`)
	disclosureCountRE = regexp.MustCompile(`(?s)class="count"[^>]*>\s*\[\s*(\d+)\s*/\s*(\d+)\s*\]\s*\[\s*Total\s+(\d+)\s*\]`)
)

// disclosedAtLayouts are the printed-timestamp layouts tried in order:
// zero-padded 12h ("Jul 27, 2026 02:52 PM"), single-digit-hour 12h
// ("Jul 27, 2026 2:52 PM"), and 24h fallbacks.
var disclosedAtLayouts = []string{
	"Jan 2, 2006 03:04 PM",
	"Jan 2, 2006 3:04 PM",
	"Jan 2, 2006 15:04",
	"Jan 2, 2006 15:04:05",
}

// manilaTZ mirrors psecal's fallback: fixed UTC+8 when tzdata is stripped
// (the Philippines has no DST).
var manilaTZ = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Manila"); err == nil {
		return loc
	}
	return time.FixedZone("PHT", 8*60*60)
}()

// ParseDisclosurePage parses one announcements/search.ax HTML fragment.
// A challenge page or a fragment without the pager span is a typed error —
// a legitimately empty result still renders the pager ([Total 0]).
func ParseDisclosurePage(htmlBody string) (*DisclosurePage, error) {
	if err := detectChallenge("announcements/search.ax", htmlBody); err != nil {
		return nil, err
	}
	cm := disclosureCountRE.FindStringSubmatch(htmlBody)
	if cm == nil {
		return nil, &ShellPageError{Endpoint: "announcements/search.ax", Reason: "result pager (count span) absent"}
	}
	page := &DisclosurePage{Rows: make([]Disclosure, 0, 50)}
	page.PageNo, _ = strconv.Atoi(cm[1])
	page.TotalPages, _ = strconv.Atoi(cm[2])
	page.TotalCount, _ = strconv.Atoi(cm[3])

	for _, m := range disclosureRowRE.FindAllStringSubmatch(htmlBody, -1) {
		cmpyID, _ := strconv.Atoi(m[1])
		d := Disclosure{
			CmpyID:     cmpyID,
			Company:    cleanText(m[2]),
			EdgeNo:     cleanText(m[3]),
			Title:      stripTags(m[4]),
			Template:   cleanText(m[5]),
			CircularNo: cleanText(m[7]),
		}
		if raw := cleanText(m[6]); raw != "" {
			for _, layout := range disclosedAtLayouts {
				if t, err := time.ParseInLocation(layout, raw, manilaTZ); err == nil {
					d.DisclosedAt = t.Format(time.RFC3339)
					break
				}
			}
			if d.DisclosedAt == "" {
				// Never leak the raw string into DisclosedAt — downstream
				// date slicing ([:10]) would corrupt on it.
				d.RawTimestamp = raw
			}
		}
		page.Rows = append(page.Rows, d)
	}
	return page, nil
}

// ViewerURL returns the human-facing disclosure viewer link for an edge_no.
func ViewerURL(edgeNo string) string {
	return "https://edge.pse.com.ph/openDiscViewer.do?edge_no=" + url.QueryEscape(edgeNo)
}

// FetchDisclosurePage POSTs one page of the announcements search. It talks
// HTTP directly because the endpoint requires a form-urlencoded body the
// generated JSON client cannot produce. Read-only despite the verb (same
// policy as the client's PostQuery family).
func FetchDisclosurePage(ctx context.Context, hc *http.Client, search DisclosureSearch, pageNo int) (*DisclosurePage, error) {
	form := url.Values{
		"companyId": {search.CompanyID},
		"keyword":   {""},
		"tmplNm":    {search.Template},
		"fromDate":  {search.FromDate},
		"toDate":    {search.ToDate},
		"pageNo":    {strconv.Itoa(pageNo)},
	}
	endpoint := disclosureSearchEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("pse-edge announcements/search.ax: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	if hc == nil {
		hc = http.DefaultClient
	}
	disclosureLimiter.Wait()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pse-edge announcements/search.ax page %d: %w", pageNo, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, disclosureMaxBody))
	if err != nil {
		return nil, fmt.Errorf("pse-edge announcements/search.ax page %d: reading response: %w", pageNo, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		disclosureLimiter.OnRateLimit()
		retryAfter, _ := time.ParseDuration(strings.TrimSpace(resp.Header.Get("Retry-After")) + "s")
		bodyPreview := string(body)
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200]
		}
		return nil, &cliutil.RateLimitError{URL: endpoint, RetryAfter: retryAfter, Body: bodyPreview}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pse-edge announcements/search.ax page %d: HTTP %d", pageNo, resp.StatusCode)
	}
	disclosureLimiter.OnSuccess()
	return ParseDisclosurePage(string(body))
}
