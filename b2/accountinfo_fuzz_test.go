package b2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzAccountInfoLoad(f *testing.F) {
	path := filepath.Join(f.TempDir(), "account.json")
	for _, seed := range []string{"", "null", "{}", `{"AuthToken":"token","AllowedBuckets":["a","b"]}`, `{"AuthToken":`, strings.Repeat(" ", 1<<20) + "{}"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		store := NewAccountInfoFile(path)
		got, ok := store.AuthData()
		var want AuthData
		valid := json.Unmarshal(content, &want) == nil
		if ok != valid || store.Err() != nil {
			t.Fatalf("load valid=%t ok=%t err=%v", valid, ok, store.Err())
		}
		if ok && (got.AuthToken != want.AuthToken || got.AccountID != want.AccountID || got.APIURL != want.APIURL || got.RecommendedPartSize != want.RecommendedPartSize) {
			t.Fatal("decoded fields differ")
		}
	})
}
