package base

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzRedactSSECJSON(f *testing.F) {
	for _, body := range []string{`{"customerKey":"secret"}`, `{"CUSTOMERKEY":"secret"}`, `{"customer\u004bey":"secret"}`, `{"nested":[{"customerKeyMd5":"secret"}]}`, `{invalid`} {
		f.Add([]byte(body))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		out := []byte(redactSensitiveJSON(body))
		var value interface{}
		if json.Unmarshal(out, &value) != nil {
			return
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
}
