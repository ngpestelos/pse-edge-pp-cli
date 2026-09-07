// Copyright 2026 Nestor G Pestelos Jr and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	xhtml "golang.org/x/net/html"
)

type disclosureDocument struct {
	FileID      string `json:"file_id"`
	ContentType string `json:"content_type"`
	Text        string `json:"text"`
	ByteLength  int    `json:"byte_length"`
}

func decodeDisclosureDocument(fileID string, raw []byte) (json.RawMessage, error) {
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
