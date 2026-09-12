package http

import (
	"errors"
	"net/http"

	"minicloud/internal/catalog"
)

func (h objectHandlers) move(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send from and to as JSON")
		return
	}
	from, ok := normalizeKey(req.From)
	if !ok || catalog.IsCollabKey(from) {
		writeError(w, http.StatusBadRequest, "bad source name")
		return
	}
	to, ok := normalizeKey(req.To)
	if !ok || catalog.IsCollabKey(to) {
		writeError(w, http.StatusBadRequest, "bad destination name")
		return
	}

	err := h.catalog.RenameObject(r.Context(), userIDFrom(r.Context()), from, to)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if errors.Is(err, catalog.ErrKeyTaken) {
		writeError(w, http.StatusConflict, "a file with that name already exists there")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not move file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"from": from, "to": to})
}
