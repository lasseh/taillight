package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lasseh/taillight/internal/juniperref"
	"github.com/lasseh/taillight/internal/model"
)

// juniperRefUploadMaxBytes caps the multipart request body size.
// Juniper reference XLSX files are small in practice (tens of thousands of
// rows compress to a few MB); 10 MiB leaves ample headroom.
const juniperRefUploadMaxBytes = 10 << 20

// juniperRefStore defines the data access needed to upsert Juniper refs.
type juniperRefStore interface {
	UpsertJuniperRefs(ctx context.Context, refs []model.JuniperNetlogRef) (int64, error)
}

// JuniperRefHandler handles admin uploads of Juniper syslog reference data.
type JuniperRefHandler struct {
	store juniperRefStore
}

// NewJuniperRefHandler creates a new JuniperRefHandler.
func NewJuniperRefHandler(s juniperRefStore) *JuniperRefHandler {
	return &JuniperRefHandler{store: s}
}

// Upload handles POST /api/v1/juniper/ref/upload.
//
// Expects a multipart/form-data body with:
//   - "file": the Juniper syslog reference XLSX.
//   - "os":   target OS identifier, either "junos" or "junos-evolved"
//     (may also be supplied as a query parameter).
func (h *JuniperRefHandler) Upload(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, juniperRefUploadMaxBytes)
	if err := r.ParseMultipartForm(juniperRefUploadMaxBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("request body exceeds %d byte limit", juniperRefUploadMaxBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_multipart", "failed to parse multipart form")
		return
	}

	osName := r.FormValue("os")
	if osName == "" {
		osName = r.URL.Query().Get("os")
	}
	if !juniperref.ValidOS(osName) {
		writeError(w, http.StatusBadRequest, "invalid_os", "os must be 'junos' or 'junos-evolved'")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "form field 'file' is required")
		return
	}
	defer file.Close() //nolint:errcheck // multipart file close error is not actionable.

	refs, err := juniperref.ParseXLSX(file, osName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse_failed", fmt.Sprintf("failed to parse xlsx: %v", err))
		return
	}
	if len(refs) == 0 {
		writeError(w, http.StatusBadRequest, "no_rows", "no reference rows found in file")
		return
	}

	affected, err := h.store.UpsertJuniperRefs(r.Context(), refs)
	if err != nil {
		if isClientGone(r) {
			return
		}
		logger.Error("upsert juniper refs failed", "err", err, "parsed", len(refs), "os", osName)
		writeError(w, http.StatusInternalServerError, "upsert_failed", "failed to store juniper references")
		return
	}

	logger.Info("juniper refs uploaded",
		"filename", header.Filename,
		"size", header.Size,
		"os", osName,
		"parsed", len(refs),
		"upserted", affected,
	)

	writeJSON(w, map[string]int64{
		"parsed":   int64(len(refs)),
		"upserted": affected,
	})
}
