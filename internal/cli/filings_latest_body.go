// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.
//
// filings latest-body: one-shot newest search row → viewer file_id → document body.

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ph-commons/pse-edge-pp-cli/internal/psecal"
	"github.com/ph-commons/pse-edge-pp-cli/internal/pseedge"
)

// latestFilingBody is the one-shot envelope: search-row edge_no plus
// downloadHtml.do body fields. Search rows themselves never carry file_id.
type latestFilingBody struct {
	EdgeNo         string                         `json:"edge_no"`
	FileID         string                         `json:"file_id"`
	DocumentFileID string                         `json:"document_file_id,omitempty"`
	Attachments    []pseedge.DisclosureAttachment `json:"attachments,omitempty"`
	ContentType    string                         `json:"content_type"`
	Text           string                         `json:"text"`
	ByteLength     int                            `json:"byte_length"`
}

func pickLatestBodyFileID(v *pseedge.DisclosureViewer) (string, error) {
	if v == nil {
		return "", notFoundErr(fmt.Errorf("disclosure viewer missing"))
	}
	if id := strings.TrimSpace(v.DocumentFileID); id != "" {
		return id, nil
	}
	if len(v.Attachments) > 0 {
		if id := strings.TrimSpace(v.Attachments[0].FileID); id != "" {
			return id, nil
		}
	}
	edge := strings.TrimSpace(v.EdgeNo)
	if edge == "" {
		edge = "(unknown)"
	}
	return "", notFoundErr(fmt.Errorf("no file_id on viewer for edge_no %s", edge))
}

func assembleLatestFilingBody(v *pseedge.DisclosureViewer, fileID string, bodyRaw json.RawMessage) (latestFilingBody, error) {
	var body disclosureDocument
	if err := json.Unmarshal(bodyRaw, &body); err != nil {
		return latestFilingBody{}, apiErr(err)
	}
	if v == nil {
		return latestFilingBody{}, notFoundErr(fmt.Errorf("disclosure viewer missing"))
	}
	out := latestFilingBody{
		EdgeNo:         v.EdgeNo,
		FileID:         fileID,
		DocumentFileID: v.DocumentFileID,
		Attachments:    v.Attachments,
		ContentType:    body.ContentType,
		Text:           body.Text,
		ByteLength:     body.ByteLength,
	}
	if out.FileID == "" {
		out.FileID = body.FileID
	}
	return out, nil
}

func newFilingsLatestBodyCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	var templateFlag string
	var fromDateFlag string
	var toDateFlag string

	cmd := &cobra.Command{
		Use:   "latest-body SYMBOL",
		Short: "Return the newest search-row filing's file_id and document body",
		Long: `One-shot for agents: take the newest announcements/search.ax row for
SYMBOL in the date window, open openDiscViewer.do, pick document_file_id
(else the first attachment file_id), and GET downloadHtml.do.

filings SYMBOL remains index-only (edge_no, no file_id on rows). Use this
command instead of scraping the viewer or chaining filings get by hand.`,
		Example: `  pse-edge-pp-cli filings latest-body GTCAP --json
  pse-edge-pp-cli filings latest-body GTCAP --from-date 01-01-2026 --json`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
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
			rc, err := resolvePSECompany(cmd.Context(), cmd, flags, dbPath, args[0])
			if err != nil {
				return err
			}

			search := pseedge.DisclosureSearch{
				CompanyID: fmt.Sprintf("%d", rc.CmpyID),
				Template:  templateFlag,
				FromDate:  fromDateFlag,
				ToDate:    toDateFlag,
			}
			hc := &http.Client{Timeout: 60 * time.Second}
			reqCtx, cancel := boundCtx(cmd.Context(), flags)
			page, err := pseedge.FetchDisclosurePage(reqCtx, hc, search, 1)
			cancel()
			if err != nil {
				return classifyAPIError(err, flags)
			}
			if page == nil || len(page.Rows) == 0 {
				return notFoundErr(fmt.Errorf("no disclosures for %s in %s..%s (search index only)", rc.Symbol, fromDateFlag, toDateFlag))
			}
			row := page.Rows[0]
			if strings.TrimSpace(row.EdgeNo) == "" {
				return notFoundErr(fmt.Errorf("newest search row for %s has empty edge_no", rc.Symbol))
			}

			reqCtx, cancel = boundCtx(cmd.Context(), flags)
			viewer, err := pseedge.FetchDisclosureViewer(reqCtx, hc, row.EdgeNo)
			cancel()
			if err != nil {
				return classifyAPIError(err, flags)
			}
			fileID, err := pickLatestBodyFileID(viewer)
			if err != nil {
				return err
			}

			c, err := flags.newClient()
			if err != nil {
				return err
			}
			data, err := c.GetWithHeaders(cmd.Context(), "/downloadHtml.do", map[string]string{"file_id": fileID}, nil)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			decoded, err := decodeDisclosureDocument(fileID, data)
			if err != nil {
				return err
			}
			out, err := assembleLatestFilingBody(viewer, fileID, decoded)
			if err != nil {
				return err
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().StringVar(&templateFlag, "template", "", `Server-side disclosure template filter, exact name (e.g. "Declaration of Cash Dividends")`)
	cmd.Flags().StringVar(&fromDateFlag, "from-date", "", "Range start, MM-DD-YYYY (default: 90 days ago)")
	cmd.Flags().StringVar(&toDateFlag, "to-date", "", "Range end, MM-DD-YYYY (default: today, Manila)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path (default: resolved data directory data.db)")
	return cmd
}
