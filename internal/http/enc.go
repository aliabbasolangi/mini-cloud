package http

import (
	"errors"
	"net/http"
	"strings"

	"minicloud/internal/catalog"
	"minicloud/internal/thumb"
)

var errBadEnc = errors.New("bad encryption headers")

type fileJSON struct {
	Key      string `json:"key"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Created  string `json:"created"`
	HasThumb bool   `json:"has_thumb"`
	EncVer   int    `json:"enc_ver"`
	EncWrap  string `json:"enc_wrap,omitempty"`
}

func fileRowJSON(o catalog.Object, key string) fileJSON {
	if key == "" {
		key = o.Key
	}
	return fileJSON{
		Key:      key,
		SHA256:   o.BlobSHA,
		Size:     o.Size,
		Created:  o.CreatedAt.Format("2006-01-02T15:04:05Z"),
		HasThumb: o.EncVer == 0 && thumb.LooksLikeImage(key),
		EncVer:   o.EncVer,
		EncWrap:  o.EncWrap,
	}
}

func parseEncHeaders(r *http.Request) (int, string, error) {
	v := strings.TrimSpace(r.Header.Get("X-Minicloud-Enc"))
	if v == "" || v == "0" {
		return 0, "", nil
	}
	if v != "1" && v != "2" {
		return 0, "", errBadEnc
	}
	wrap := strings.TrimSpace(r.Header.Get("X-Minicloud-Wrap"))
	if wrap == "" || len(wrap) > 2048 {
		return 0, "", errBadEnc
	}
	if v == "2" {
		return 2, wrap, nil
	}
	return 1, wrap, nil
}
