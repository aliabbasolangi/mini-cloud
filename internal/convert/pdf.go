package convert

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

func TextToPDF(text string) ([]byte, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := wrapPDFLines(text, 90)
	if len(lines) == 0 {
		lines = []string{""}
	}
	const perPage = 60
	var pages [][]string
	for i := 0; i < len(lines); i += perPage {
		end := i + perPage
		if end > len(lines) {
			end = len(lines)
		}
		pages = append(pages, lines[i:end])
	}

	var contentObjs []string
	for _, page := range pages {
		var stream strings.Builder
		stream.WriteString("BT\n/F1 11 Tf\n14 TL\n72 760 Td\n")
		for _, line := range page {
			stream.WriteString("(")
			stream.WriteString(pdfEscape(toWinAnsi(line)))
			stream.WriteString(") '\n")
		}
		stream.WriteString("ET\n")
		contentObjs = append(contentObjs, stream.String())
	}
	return assemblePDF(contentObjs, nil)
}

func JPEGToPDF(jpegBytes []byte, w, h int) ([]byte, error) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	pageW, pageH := 612.0, 792.0
	iw, ih := float64(w), float64(h)
	scale := pageW / iw
	if ih*scale > pageH {
		scale = pageH / ih
	}
	dw, dh := iw*scale, ih*scale
	x := (pageW - dw) / 2
	y := (pageH - dh) / 2
	stream := fmt.Sprintf("q\n%.2f 0 0 %.2f %.2f %.2f cm\n/Im0 Do\nQ\n", dw, dh, x, y)
	return assemblePDF([]string{stream}, &pdfImage{data: jpegBytes, w: w, h: h})
}

type pdfImage struct {
	data []byte
	w, h int
}

func assemblePDF(contents []string, img *pdfImage) ([]byte, error) {
	type obj struct{ n int; body string }
	var objs []obj
	add := func(body string) int {
		n := len(objs) + 1
		objs = append(objs, obj{n: n, body: body})
		return n
	}

	fontN := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	var imgN int
	if img != nil {
		imgN = add(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", img.w, img.h, len(img.data)) + string(img.data) + "\nendstream")
	}

	var pageNs []int
	for _, c := range contents {
		cN := add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
		res := fmt.Sprintf("/Font << /F1 %d 0 R >>", fontN)
		if imgN != 0 {
			res += fmt.Sprintf(" /XObject << /Im0 %d 0 R >>", imgN)
		}
		pageNs = append(pageNs, add(fmt.Sprintf("<< /Type /Page /Parent PAGES 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << %s >> >>", cN, res)))
	}

	var kids []string
	for _, n := range pageNs {
		kids = append(kids, fmt.Sprintf("%d 0 R", n))
	}
	pagesBody := fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(pageNs), strings.Join(kids, " "))
	pagesN := add(pagesBody)
	catN := add(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesN))

	for i := range objs {
		objs[i].body = strings.ReplaceAll(objs[i].body, "PAGES 0 R", fmt.Sprintf("%d 0 R", pagesN))
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs)+1)
	for _, o := range objs {
		offs[o.n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", o.n, o.body)
	}
	startxref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catN, startxref)
	return buf.Bytes(), nil
}

func pdfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}

func toWinAnsi(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\t' {
			b.WriteString("    ")
			continue
		}
		if r < 32 || r > 126 {
			if unicode.IsSpace(r) {
				b.WriteByte(' ')
				continue
			}
			b.WriteByte('?')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func wrapPDFLines(text string, width int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for len(line) > width {
			out = append(out, line[:width])
			line = line[width:]
		}
		out = append(out, line)
	}
	return out
}

var pdfString = regexp.MustCompile(`\((?:\\.|[^\\)])*\)`)

func PDFToText(src []byte) string {
	matches := pdfString.FindAll(src, -1)
	var parts []string
	for _, m := range matches {
		s := string(m)
		if len(s) < 2 {
			continue
		}
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, "\\(", "(")
		s = strings.ReplaceAll(s, "\\)", ")")
		s = strings.ReplaceAll(s, "\\\\", "\\")
		if s == "" || strings.HasPrefix(s, "/") {
			continue
		}
		if _, err := strconv.Atoi(s); err == nil && len(s) < 4 {
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}
