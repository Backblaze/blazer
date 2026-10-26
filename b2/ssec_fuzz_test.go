package b2

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"testing"
)

func FuzzSSECKey(f *testing.F) {
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 32))
	f.Fuzz(func(t *testing.T, raw []byte) {
		key, err := NewSSECKey(raw)
		if len(raw) != 32 {
			if err == nil {
				t.Fatal("invalid key length accepted")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		encoded, digest, err := key.encoded()
		sum := md5.Sum(raw)
		if err != nil || encoded != base64.StdEncoding.EncodeToString(raw) || digest != base64.StdEncoding.EncodeToString(sum[:]) {
			t.Fatal("wire encoding mismatch")
		}
	})
}
