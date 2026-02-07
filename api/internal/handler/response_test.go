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

func TestMustJSON_Error(t *testing.T) {
	// channels cannot be marshaled to JSON.
	ch := make(chan int)
	b, ok := mustJSON(ch)
	if ok {
		t.Error("mustJSON should return false for unmarshalable value")
	}
	if b != nil {
		t.Errorf("mustJSON should return nil bytes on error, got %v", b)
	}
}

func TestEmptySlice(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		var s []string
		got := emptySlice(s)
		if got == nil {
			t.Fatal("expected non-nil slice")
		}
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
		// Verify JSON marshals as [] not null.
		b, _ := json.Marshal(got)
		if string(b) != "[]" {
			t.Errorf("JSON = %s, want []", b)
		}
	})

	t.Run("non-nil slice", func(t *testing.T) {
		s := []string{"a", "b"}
		got := emptySlice(s)
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
	})

	t.Run("empty non-nil slice", func(t *testing.T) {
		s := []int{}
		got := emptySlice(s)
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

func TestItemResponse(t *testing.T) {
	w := httptest.NewRecorder()
	resp := itemResponse{Data: map[string]string{"name": "test"}}

	writeJSON(w, resp)

	var got struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Data["name"] != "test" {
		t.Errorf("data.name = %q, want %q", got.Data["name"], "test")
	}
}

func TestWriteError_StatusCodes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		msg    string
	}{
		{name: "not found", status: http.StatusNotFound, code: "not_found", msg: "resource not found"},
		{name: "internal", status: http.StatusInternalServerError, code: "internal_error", msg: "something broke"},
		{name: "forbidden", status: http.StatusForbidden, code: "forbidden", msg: "access denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, tt.status, tt.code, tt.msg)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}

			var got errorBody
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", got.Error.Code, tt.code)
			}
			if got.Error.Message != tt.msg {
				t.Errorf("message = %q, want %q", got.Error.Message, tt.msg)
			}
		})
	}
}
