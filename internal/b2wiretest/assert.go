package b2wiretest

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// AssertJSON checks a non-empty, dot-separated path in a request's JSON object body.
// Dots always separate path segments; keys containing literal dots cannot be addressed.
func AssertJSON(t testing.TB, r Request, path string, want any) {
	t.Helper()
	if path == "" {
		t.Errorf("%s body: empty JSON path", r.Call)
		return
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("%s body %s: encode want: %v", r.Call, path, err)
		return
	}
	var normalized any
	if err := json.Unmarshal(wantJSON, &normalized); err != nil {
		t.Fatalf("%s body %s: decode want: %v", r.Call, path, err)
		return
	}
	got, ok := jsonPath(r.JSON, path)
	if !ok {
		t.Errorf("%s body %s: missing, want %s", r.Call, path, wantJSON)
		return
	}
	if !reflect.DeepEqual(got, normalized) {
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s body %s: got %s, want %s", r.Call, path, gotJSON, wantJSON)
	}
}

// AssertNoJSON checks that a non-empty, dot-separated path is absent from a request body.
// It has the same path limitations as AssertJSON.
func AssertNoJSON(t testing.TB, r Request, path string) {
	t.Helper()
	if path == "" {
		t.Errorf("%s body: empty JSON path", r.Call)
		return
	}
	if got, ok := jsonPath(r.JSON, path); ok {
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s body %s: got %s, want missing", r.Call, path, gotJSON)
	}
}

// AssertHeader checks one request header's value.
func AssertHeader(t testing.TB, r Request, name, want string) {
	t.Helper()
	if got := r.Header.Get(name); got != want {
		t.Errorf("%s header %s: got %q, want %q", r.Call, name, got, want)
	}
}

// AssertQuery checks one request query parameter's value.
func AssertQuery(t testing.TB, r Request, name, want string) {
	t.Helper()
	if _, ok := r.Query[name]; !ok {
		t.Errorf("%s query %s: missing, want %q", r.Call, name, want)
		return
	}
	if got := r.Query.Get(name); got != want {
		t.Errorf("%s query %s: got %q, want %q", r.Call, name, got, want)
	}
}

func jsonPath(root map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	var current any = root
	for _, part := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[part]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}
