package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/model"
)

// metaSearchRecorder records the search argument the meta list and analysis
// picker handlers pass to the store. Only those list methods are
// implemented; the embedded interfaces are nil, so any other call panics.
type metaSearchRecorder struct {
	SrvlogStore
	NetlogStore
	AppLogStore
	AnalysisReportStore
	got    string
	called bool
}

func (m *metaSearchRecorder) record(search string) ([]string, error) {
	m.got, m.called = search, true
	return nil, nil
}

func (m *metaSearchRecorder) ListSrvlogHosts(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListSrvlogPrograms(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListSrvlogTags(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListNetlogHosts(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListNetlogPrograms(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListNetlogTags(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListServices(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListComponents(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListAppLogHosts(_ context.Context, s string) ([]string, error) {
	return m.record(s)
}

func (m *metaSearchRecorder) ListAnalysisHostEntries(_ context.Context, _, s string) ([]model.AnalysisHostEntry, error) {
	_, err := m.record(s)
	return nil, err
}

func (m *metaSearchRecorder) ListAnalysisServiceEntries(_ context.Context, s string) ([]model.AnalysisServiceEntry, error) {
	_, err := m.record(s)
	return nil, err
}

// metaSearchEndpoint is one endpoint that accepts the search parameter, with
// the handler that serves it and any query it needs besides search.
type metaSearchEndpoint struct {
	path  string
	query string
	serve func(*metaSearchRecorder) http.HandlerFunc
}

var metaSearchEndpoints = []metaSearchEndpoint{
	{"/api/v1/srvlog/meta/hosts", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewSrvlogMetaHandler(s).Hosts }},
	{"/api/v1/srvlog/meta/programs", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewSrvlogMetaHandler(s).Programs }},
	{"/api/v1/srvlog/meta/tags", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewSrvlogMetaHandler(s).Tags }},
	{"/api/v1/netlog/meta/hosts", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewNetlogMetaHandler(s).Hosts }},
	{"/api/v1/netlog/meta/programs", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewNetlogMetaHandler(s).Programs }},
	{"/api/v1/netlog/meta/tags", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewNetlogMetaHandler(s).Tags }},
	{"/api/v1/applog/meta/services", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewAppLogMetaHandler(s).Services }},
	{"/api/v1/applog/meta/components", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewAppLogMetaHandler(s).Components }},
	{"/api/v1/applog/meta/hosts", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewAppLogMetaHandler(s).Hosts }},
	{"/api/v1/analysis/hosts", "feed=netlog", func(s *metaSearchRecorder) http.HandlerFunc { return NewAnalysisHandler(s, nil).Hosts }},
	{"/api/v1/analysis/services", "", func(s *metaSearchRecorder) http.HandlerFunc { return NewAnalysisHandler(s, nil).Services }},
}

// goldenMetaSearchDoc describes the search parameter of the meta list and
// analysis picker endpoints. TestGoldenMetaSearchSatisfiesServerRules proves
// it against the real handlers.
type goldenMetaSearchDoc struct {
	Param     string   `json:"param"`
	MaxLength int      `json:"max_length"`
	Endpoints []string `json:"endpoints"`
}

func goldenMetaSearch() goldenMetaSearchDoc {
	doc := goldenMetaSearchDoc{Param: "search", MaxLength: 500}
	for _, ep := range metaSearchEndpoints {
		doc.Endpoints = append(doc.Endpoints, ep.path)
	}
	return doc
}

func TestGoldenMetaSearch(t *testing.T) {
	checkGolden(t, "meta_search_params.json", goldenMetaSearch())
}

// TestGoldenMetaSearchSatisfiesServerRules sends each endpoint in the golden
// no search, a search, and searches at and past the advertised max length.
func TestGoldenMetaSearchSatisfiesServerRules(t *testing.T) {
	doc := goldenMetaSearch()
	tests := []struct {
		name       string
		search     *string // nil: parameter absent
		wantStatus int
	}{
		{"absent", nil, http.StatusOK},
		{"substring", goldenPtr("core-1"), http.StatusOK},
		{"at the max length", goldenPtr(strings.Repeat("a", doc.MaxLength)), http.StatusOK},
		{"past the max length", goldenPtr(strings.Repeat("a", doc.MaxLength+1)), http.StatusBadRequest},
	}
	for _, ep := range metaSearchEndpoints {
		for _, tt := range tests {
			t.Run(ep.path+"/"+tt.name, func(t *testing.T) {
				q, err := url.ParseQuery(ep.query)
				if err != nil {
					t.Fatalf("parse query: %v", err)
				}
				if tt.search != nil {
					q.Set(doc.Param, *tt.search)
				}
				store := &metaSearchRecorder{}
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, ep.path+"?"+q.Encode(), nil)
				rec := httptest.NewRecorder()
				ep.serve(store)(rec, req)

				if rec.Code != tt.wantStatus {
					t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body.String())
				}
				if tt.wantStatus != http.StatusOK {
					if store.called {
						t.Errorf("store called for a refused search")
					}
					return
				}
				want := ""
				if tt.search != nil {
					want = *tt.search
				}
				if !store.called || store.got != want {
					t.Errorf("store got search %q (called %v), want %q", store.got, store.called, want)
				}
			})
		}
	}
}
