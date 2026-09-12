package convert

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
)

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func TextToDocx(text string) ([]byte, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	body.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	body.WriteString(`<w:body>`)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		body.WriteString(xmlEscape(line))
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	body.WriteString(`<w:sectPr/></w:body></w:document>`)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml": body.String(),
	}
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var docxText = regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)

func DocxToText(src []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return "", err
		}
		raw = bytes.ReplaceAll(raw, []byte("</w:p>"), []byte("\n"))
		matches := docxText.FindAllSubmatch(raw, -1)
		var parts []string
		for _, m := range matches {
			parts = append(parts, string(m[1]))
		}
		out := strings.Join(parts, "")
		out = strings.ReplaceAll(out, "\r", "")
		if out == "" {
			out = string(docxText.ReplaceAll(raw, []byte("$1")))
		}
		return out, nil
	}
	return "", ErrUnsupported
}
