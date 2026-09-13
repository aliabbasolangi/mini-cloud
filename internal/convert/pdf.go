package convert

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"io"
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

var (
	pdfShow  = regexp.MustCompile(`\((?:\\.|[^\\)])*\)\s*(?:Tj|'|")`)
	pdfTJ    = regexp.MustCompile(`\[(?:[^\[\]]|\[[^\[\]]*\])*\]\s*TJ`)
	pdfLit   = regexp.MustCompile(`\((?:\\.|[^\\)])*\)`)
	pdfFlate = regexp.MustCompile(`(?i)/FlateDecode`)
)

func PDFToText(src []byte) string {
	var parts []string
	for _, chunk := range pdfContentChunks(src) {
		parts = append(parts, textFromPDFContent(chunk)...)
	}
	// Last resort: literals in uncompressed objects only, never raw binary.
	if len(parts) == 0 {
		parts = append(parts, textFromPDFContent(src)...)
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

func pdfContentChunks(src []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(src); {
		start := bytes.Index(src[i:], []byte("stream"))
		if start < 0 {
			break
		}
		abs := i + start
		dictFrom := abs - 800
		if dictFrom < i {
			dictFrom = i
		}
		dict := src[dictFrom:abs]
		dataStart := abs + 6
		if dataStart < len(src) && src[dataStart] == '\r' {
			dataStart++
		}
		if dataStart < len(src) && src[dataStart] == '\n' {
			dataStart++
		}
		end := bytes.Index(src[dataStart:], []byte("endstream"))
		if end < 0 {
			break
		}
		data := bytes.TrimRight(src[dataStart:dataStart+end], "\r\n")
		if pdfFlate.Match(dict) {
			if dec, err := inflatePDF(data); err == nil {
				data = dec
			} else {
				i = dataStart + end + 9
				continue
			}
		}
		out = append(out, data)
		i = dataStart + end + 9
	}
	return out
}

func inflatePDF(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err == nil {
		defer zr.Close()
		return io.ReadAll(zr)
	}
	fr := flate.NewReader(bytes.NewReader(data))
	defer fr.Close()
	return io.ReadAll(fr)
}

func textFromPDFContent(src []byte) []string {
	var parts []string
	for _, m := range pdfTJ.FindAll(src, -1) {
		for _, lit := range pdfLit.FindAll(m, -1) {
			if s := decodePDFLiteral(lit); s != "" {
				parts = append(parts, s)
			}
		}
		parts = append(parts, "\n")
	}
	for _, m := range pdfShow.FindAll(src, -1) {
		lit := pdfLit.Find(m)
		if lit == nil {
			continue
		}
		s := decodePDFLiteral(lit)
		if s == "" {
			continue
		}
		parts = append(parts, s)
		if bytes.Contains(m, []byte("'")) || bytes.Contains(m, []byte(`"`)) {
			parts = append(parts, "\n")
		} else {
			parts = append(parts, " ")
		}
	}
	return parts
}

func decodePDFLiteral(raw []byte) string {
	if len(raw) < 2 || raw[0] != '(' || raw[len(raw)-1] != ')' {
		return ""
	}
	s := string(raw[1 : len(raw)-1])
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b', 'f':
			b.WriteByte(' ')
		case '(', ')', '\\':
			b.WriteByte(s[i])
		case '\n', '\r':
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		default:
			if s[i] >= '0' && s[i] <= '7' {
				n, used := pdfOctal(s[i:])
				b.WriteByte(byte(n))
				i += used - 1
			}
		}
	}
	out := strings.ReplaceAll(b.String(), "\r", "")
	if !usefulPDFText(out) {
		return ""
	}
	if _, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && len(out) < 4 {
		return ""
	}
	return out
}

func pdfOctal(s string) (n, used int) {
	for used < 3 && used < len(s) && s[used] >= '0' && s[used] <= '7' {
		n = n*8 + int(s[used]-'0')
		used++
	}
	return n, used
}

func usefulPDFText(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			letters++
		}
	}
	return letters >= 2
}
