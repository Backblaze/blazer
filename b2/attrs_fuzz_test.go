package b2

import (
	"strconv"
	"testing"
)

func FuzzAttrsFromFileInfo(f *testing.F) {
	for _, value := range []string{"0", "-1", "1700000000123", "9223372036854775807", "not-millis"} {
		f.Add(value, "sha", "user")
	}
	f.Fuzz(func(t *testing.T, millis, sha, user string) {
		info := map[string]string{"src_last_modified_millis": millis, "large_file_sha1": sha, "custom": user}
		file := &beFileInfo{info: info, status: "upload"}
		attrs, err := attrsFromFileInfo(file)
		n, parseErr := strconv.ParseInt(millis, 10, 64)
		if (err == nil) != (parseErr == nil) {
			t.Fatalf("timestamp acceptance differs: %v", err)
		}
		if info["src_last_modified_millis"] != millis || info["large_file_sha1"] != sha || info["custom"] != user {
			t.Fatal("source metadata mutated")
		}
		if err != nil {
			return
		}
		if attrs.LastModified.UnixMilli() != n || attrs.SHA1 != sha || attrs.Info["large_file_sha1"] != sha {
			t.Fatal("metadata changed")
		}
		attrs.Info["custom"] = "mutated"
		if info["custom"] != user {
			t.Fatal("public map aliases cache")
		}
	})
}
