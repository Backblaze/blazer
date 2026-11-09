package base

import (
	"encoding/json"
	"flag"
	"strings"
	"testing"
)

func FuzzRedactSSECJSON(f *testing.F) {
	for _, body := range []string{`{"customerKey":"secret"}`, `{"CUSTOMERKEY":"secret"}`, `{"customer\u004bey":"secret"}`, `{"nested":[{"customerKeyMd5":"secret"}]}`, `{invalid`} {
		f.Add([]byte(body))
	}
	count := 0
	f.Fuzz(func(t *testing.T, body []byte) {
		var input interface{}
		if json.Unmarshal(body, &input) != nil {
			return
		}
		count++
		out := []byte(redactSensitiveJSON(body))
		if got := redactSensitiveJSON(out); got != string(out) {
			t.Fatalf("redacting twice changed the body: %q", got)
		}
		var value interface{}
		if err := json.Unmarshal(out, &value); err != nil {
			t.Fatalf("redacted JSON is invalid: %q: %v", out, err)
		}
		var check func(interface{})
		check = func(value interface{}) {
			switch v := value.(type) {
			case map[string]interface{}:
				for key, item := range v {
					if (strings.EqualFold(key, "customerKey") || strings.EqualFold(key, "customerKeyMd5")) && item != "[redacted]" {
						t.Fatalf("secret not redacted for key %q", key)
					}
					check(item)
				}
			case []interface{}:
				for _, item := range v {
					check(item)
				}
			}
		}
		check(value)
	})
	if count == 0 && flag.Lookup("test.fuzz").Value.String() == "" {
		f.Fatal("no valid JSON input reached the redaction invariant")
	}
}
