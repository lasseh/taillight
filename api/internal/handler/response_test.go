package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	writeJSON(w, data)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("body key = %q, want %q", got["key"], "value")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()

	writeError(w, http.StatusBadRequest, "invalid_input", "bad request body")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var got errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Error.Code != "invalid_input" {
		t.Errorf("error code = %q, want %q", got.Error.Code, "invalid_input")
	}
	if got.Error.Message != "bad request body" {
		t.Errorf("error message = %q, want %q", got.Error.Message, "bad request body")
	}
}

func TestMustJSON(t *testing.T) {
	data := map[string]int{"id": 42}
	b, ok := mustJSON(data)
	if !ok {
		t.Fatal("mustJSON returned not ok")
	}
	if len(b) == 0 {
		t.Fatal("mustJSON returned empty bytes")
	}

	var got map[string]int
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["id"] != 42 {
		t.Errorf("id = %d, want 42", got["id"])
	}
}

func TestListResponse_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	resp := listResponse{
		Data:    []string{},
		HasMore: false,
	}

	writeJSON(w, resp)

	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got["data"]) != "[]" {
		t.Errorf("data = %s, want []", got["data"])
	}
	if string(got["has_more"]) != "false" {
		t.Errorf("has_more = %s, want false", got["has_more"])
	}
	if _, ok := got["cursor"]; ok {
		t.Error("cursor should be omitted when nil")
	}
}

func TestListResponse_WithCursor(t *testing.T) {
	w := httptest.NewRecorder()
	cursor := "abc123"
	resp := listResponse{
		Data:    []string{"a"},
		Cursor:  &cursor,
		HasMore: true,
	}

	writeJSON(w, resp)

	var got struct {
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"has_more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Cursor != "abc123" {
		t.Errorf("cursor = %q, want %q", got.Cursor, "abc123")
	}
	if !got.HasMore {
		t.Error("has_more = false, want true")
	}
}
