// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

type disclosureDocument struct {
	FileID      string `json:"file_id"`
	ContentType string `json:"content_type"`
	Text        string `json:"text"`
	ByteLength  int    `json:"byte_length"`
}

func decodeDisclosureDocument(ctx context.Context, fileID string, raw []byte) (json.RawMessage, error) {
	raw = unwrapPrintingPressBinary(raw)
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, notFoundErr(fmt.Errorf("disclosure document %s: empty body", fileID))
	}

	doc := disclosureDocument{
		FileID:     fileID,
		ByteLength: len(raw),
	}
	if looksLikePDF(raw) {
		doc.ContentType = "application/pdf"
		text, err := extractDisclosurePDFText(ctx, raw)
		if err != nil {
			return nil, apiErr(fmt.Errorf("disclosure document %s: %w", fileID, err))
		}
		doc.Text = text
	} else {
		doc.ContentType = "text/html"
		doc.Text = visibleHTMLBodyText(raw)
		if doc.Text == "" {
			return nil, notFoundErr(fmt.Errorf("disclosure document %s: no extractable text", fileID))
		}
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, apiErr(err)
	}
	return json.RawMessage(out), nil
}

func looksLikePDF(raw []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(raw, "\r\n\t "), []byte("%PDF-"))
}

// unwrapPrintingPressBinary undoes client.wrapBinaryResponse so PDF magic
// sniffing sees the attachment bytes, not the JSON envelope.
func unwrapPrintingPressBinary(raw []byte) []byte {
	var env struct {
		PPBinary bool   `json:"_pp_binary"`
		Encoding string `json:"encoding"`
		Data     string `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || !env.PPBinary {
		return raw
	}
	if !strings.EqualFold(env.Encoding, "base64") {
		return raw
	}
	decoded, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return raw
	}
	return decoded
}

func visibleHTMLBodyText(raw []byte) string {
	parsed, err := parseHTMLDocument(raw, "text/html")
	if err != nil {
		return cleanHTMLText(string(raw))
	}
	var body *xhtml.Node
	walkHTML(parsed, func(n *xhtml.Node) {
		if body != nil || n == nil || n.Type != xhtml.ElementNode {
			return
		}
		if strings.EqualFold(n.Data, "body") {
			body = n
		}
	})
	if body != nil {
		return cleanHTMLText(nodeTextSuppressing(body))
	}
	return cleanHTMLText(nodeTextSuppressing(parsed))
}

// Poppler reads stdin and writes UTF-8 text to stdout; attachment bytes never
// reach command output. Bound execution independently of the HTTP request.
func extractDisclosurePDFText(ctx context.Context, raw []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", "-", "-")
	cmd.Stdin = bytes.NewReader(raw)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("PDF text extraction: %w", ctx.Err())
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("PDF text extraction requires pdftotext; install Poppler and ensure pdftotext is on PATH")
	}
	if err != nil {
		return "", fmt.Errorf("PDF text extraction failed (invalid, encrypted, or unreadable PDF): %w", err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", fmt.Errorf("PDF has no extractable text layer; scanned documents require OCR")
	}
	return text, nil
}
