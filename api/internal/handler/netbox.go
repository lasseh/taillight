package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/netbox"
)

// NetboxHandler enriches netlog events with information from Netbox.
// Lookups happen only on the detail page (this handler) — never on list/SSE.
type NetboxHandler struct {
	client *netbox.Client
	store  NetboxStore
	logger *slog.Logger
}

// NewNetboxHandler constructs a NetboxHandler.
func NewNetboxHandler(client *netbox.Client, store NetboxStore, logger *slog.Logger) *NetboxHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &NetboxHandler{client: client, store: store, logger: logger}
}

// netboxEnrichment is the JSON payload returned by EnrichNetlog.
type netboxEnrichment struct {
	Entities []netbox.Entity `json:"entities"`
	Lookups  []netbox.Lookup `json:"lookups"`
}

// perLookupTimeout caps each individual Netbox call within the parallel
// fan-out. The Client's own httpClient.Timeout already bounds the request,
// but we add this as a defensive cap on contexts derived from the request.
const perLookupTimeout = 5 * time.Second

// EnrichNetlog handles GET /api/v1/netlog/{id}/netbox.
//
// It fetches the netlog event, extracts entities from its message + hostname
// using the jink lexer, dispatches concurrent Netbox lookups, and returns
// the assembled envelope. Per-entity errors are surfaced via Lookup.Error;
// they don't fail the request.
func (h *NetboxHandler) EnrichNetlog(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid event id")
		return
	}

	event, err := h.store.GetNetlog(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("netbox enrich: fetch netlog failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get event")
		return
	}

	entities := netbox.Extract(event.Message, event.Hostname)
	lookups := h.runLookups(r.Context(), entities)

	writeJSON(w, itemResponse{Data: netboxEnrichment{
		Entities: entities,
		Lookups:  lookups,
	}})
}

// runLookups dispatches one Netbox call per entity in parallel and returns
// results in entity order.
func (h *NetboxHandler) runLookups(ctx context.Context, entities []netbox.Entity) []netbox.Lookup {
	results := make([]netbox.Lookup, len(entities))
	var wg sync.WaitGroup
	for i, ent := range entities {
		wg.Add(1)
		go func(i int, ent netbox.Entity) {
			defer wg.Done()
			results[i] = h.lookupOne(ctx, ent)
		}(i, ent)
	}
	wg.Wait()
	return results
}

func (h *NetboxHandler) lookupOne(parent context.Context, ent netbox.Entity) netbox.Lookup {
	ctx, cancel := context.WithTimeout(parent, perLookupTimeout)
	defer cancel()

	out := netbox.Lookup{Entity: ent}
	switch ent.Type {
	case netbox.EntityDevice:
		d, err := h.client.LookupDevice(ctx, ent.Value)
		if err != nil {
			out.Error = err.Error()
			break
		}
		if d != nil {
			out.Found = true
			out.Data = &netbox.LookupData{Device: d}
		}
	case netbox.EntityIP:
		ip, err := h.client.LookupIP(ctx, ent.Value)
		if err != nil {
			out.Error = err.Error()
			break
		}
		if ip != nil {
			out.Found = true
			out.Data = &netbox.LookupData{IP: ip}
		}
	case netbox.EntityPrefix:
		p, err := h.client.LookupPrefix(ctx, ent.Value)
		if err != nil {
			out.Error = err.Error()
			break
		}
		if p != nil {
			out.Found = true
			out.Data = &netbox.LookupData{Prefix: p}
		}
	case netbox.EntityASN:
		a, err := h.client.LookupASN(ctx, ent.Value)
		if err != nil {
			out.Error = err.Error()
			break
		}
		if a != nil {
			out.Found = true
			out.Data = &netbox.LookupData{ASN: a}
		}
	case netbox.EntityInterface:
		iface, err := h.client.LookupInterface(ctx, ent.Context["device"], ent.Value)
		if err != nil {
			out.Error = err.Error()
			break
		}
		if iface != nil {
			out.Found = true
			out.Data = &netbox.LookupData{Interface: iface}
		}
	default:
		out.Error = "unknown entity type"
	}

	if out.Error != "" {
		h.logger.Warn("netbox lookup failed", "type", ent.Type, "value", ent.Value, "err", out.Error)
	}
	return out
}
