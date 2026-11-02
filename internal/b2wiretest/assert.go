package b2wiretest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// AssertJSON decodes r.Body and checks a non-empty, dot-separated path in its JSON object.
// A hand-built Request must set Body. Numbers compare by value with large integers kept exact.
// Dots always separate path segments; keys containing literal dots cannot be addressed.
func AssertJSON(t testing.TB, r Request, path string, want any) {
	t.Helper()
	if path == "" {
		t.Errorf("%s body: empty JSON path", r.Call)
		return
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Errorf("%s body %s: encode want: %v", r.Call, path, err)
		return
	}
	var normalized any
	if err := decodeSingleJSON(wantJSON, &normalized); err != nil {
		t.Errorf("%s body %s: decode want: %v", r.Call, path, err)
		return
	}
	recorded, ok := decodeJSONObject(r.Body)
	if !ok {
		t.Errorf("%s body %s: missing, want %v", r.Call, path, want)
		return
	}
	got, ok := jsonPath(recorded, path)
	if !ok {
		t.Errorf("%s body %s: missing, want %s", r.Call, path, wantJSON)
		return
	}
	if !jsonValuesEqual(got, normalized) {
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s body %s: got %s, want %s", r.Call, path, gotJSON, wantJSON)
	}
}

// AssertNoJSON checks that a non-empty, dot-separated path is absent from r.Body.
// An empty Body means the field is absent; a non-empty Body must be a single JSON object.
// It has the same path limitations as AssertJSON.
func AssertNoJSON(t testing.TB, r Request, path string) {
	t.Helper()
	if path == "" {
		t.Errorf("%s body: empty JSON path", r.Call)
		return
	}
	if len(r.Body) == 0 {
		return
	}
	recorded, ok := decodeJSONObject(r.Body)
	if !ok {
		t.Errorf("%s body %s: body is not a JSON object", r.Call, path)
		return
	}
	if got, ok := jsonPath(recorded, path); ok {
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s body %s: got %s, want missing", r.Call, path, gotJSON)
	}
}

func decodeJSONObject(body []byte) (map[string]any, bool) {
	var object map[string]any
	if err := decodeSingleJSON(body, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func decodeSingleJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func jsonValuesEqual(got, want any) bool {
	if gotNumber, ok := got.(json.Number); ok {
		wantNumber, ok := want.(json.Number)
		if !ok {
			return false
		}
		gotRat, gotOK := new(big.Rat).SetString(gotNumber.String())
		wantRat, wantOK := new(big.Rat).SetString(wantNumber.String())
		return gotOK && wantOK && gotRat.Cmp(wantRat) == 0
	}
	switch got := got.(type) {
	case []any:
		want, ok := want.([]any)
		if !ok || len(got) != len(want) {
			return false
		}
		for i := range got {
			if !jsonValuesEqual(got[i], want[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		want, ok := want.(map[string]any)
		if !ok || len(got) != len(want) {
			return false
		}
		for key, value := range got {
			wantValue, ok := want[key]
			if !ok || !jsonValuesEqual(value, wantValue) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(got, want)
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
