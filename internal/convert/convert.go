package convert

import (
	"bytes"
	"errors"
	"path"
	"strings"
)

var (
	ErrUnsupported = errors.New("that conversion is not supported yet")
	ErrEmpty       = errors.New("file is empty")
	ErrNoText      = errors.New("could not read the text from this PDF")
)

type Result struct {
	Bytes       []byte
	Name        string
	ContentType string
}

func Ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
}

func Stem(name string) string {
	base := path.Base(name)
	ext := path.Ext(base)
	if ext == "" {
		return base
	}
	return strings.TrimSuffix(base, ext)
}

func NormalizeTo(to string) string {
	to = strings.ToLower(strings.TrimSpace(to))
	to = strings.TrimPrefix(to, ".")
	if to == "jpeg" {
		return "jpg"
	}
	return to
}

func Kind(name string) string {
	switch Ext(name) {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp":
		return "image"
	case "docx":
		return "docx"
	case "pdf":
		return "pdf"
	case "txt", "md", "html", "htm", "csv", "json", "log", "xml":
		return "text"
	default:
		return "unknown"
	}
}

func Targets(srcName string) []string {
	switch Kind(srcName) {
	case "image":
		return []string{"png", "jpg", "gif", "pdf"}
	case "text":
		return []string{"txt", "md", "html", "pdf", "docx"}
	case "docx":
		return []string{"txt", "md", "html", "pdf"}
	case "pdf":
		return []string{"txt", "docx"}
	default:
		return []string{"txt", "pdf", "docx"}
	}
}

func Convert(src []byte, srcName, to string) (*Result, error) {
	if len(src) == 0 {
		return nil, ErrEmpty
	}
	to = NormalizeTo(to)
	if to == "" {
		return nil, ErrUnsupported
	}
	from := Ext(srcName)
	if from == "jpeg" {
		from = "jpg"
	}
	if from == to {
		return &Result{Bytes: src, Name: srcName, ContentType: contentType(to)}, nil
	}

	switch {
	case Kind(srcName) == "image":
		return convertImage(src, srcName, to)
	case from == "docx":
		text, err := DocxToText(src)
		if err != nil {
			return nil, err
		}
		return fromText(text, srcName, to)
	case from == "pdf":
		text := strings.TrimSpace(PDFToText(src))
		if text == "" {
			return nil, ErrNoText
		}
		return fromText(text, srcName, to)
	default:
		return fromText(string(src), srcName, to)
	}
}

func fromText(text, srcName, to string) (*Result, error) {
	name := Stem(srcName) + "." + to
	switch to {
	case "txt", "md", "csv", "json", "log":
		return &Result{Bytes: []byte(text), Name: name, ContentType: "text/plain; charset=utf-8"}, nil
	case "html", "htm":
		return &Result{Bytes: []byte(textToHTML(text)), Name: Stem(srcName) + ".html", ContentType: "text/html; charset=utf-8"}, nil
	case "pdf":
		out, err := TextToPDF(text)
		if err != nil {
			return nil, err
		}
		return &Result{Bytes: out, Name: name, ContentType: "application/pdf"}, nil
	case "docx":
		out, err := TextToDocx(text)
		if err != nil {
			return nil, err
		}
		return &Result{Bytes: out, Name: name, ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}, nil
	default:
		return nil, ErrUnsupported
	}
}

func textToHTML(text string) string {
	var b bytes.Buffer
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Converted</title></head><body><pre>")
	b.WriteString(xmlEscape(text))
	b.WriteString("</pre></body></html>")
	return b.String()
}

func contentType(to string) string {
	switch to {
	case "png":
		return "image/png"
	case "jpg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "html", "htm":
		return "text/html; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
