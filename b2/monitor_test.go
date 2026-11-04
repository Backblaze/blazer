package b2

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMethodListCountByMethod(t *testing.T) {
	calls := MethodList{{name: "b2_list_buckets"}, {name: "b2_upload_file"}, {name: "b2_list_buckets"}}
	want := map[string]int{"b2_list_buckets": 2, "b2_upload_file": 1}
	if got := calls.CountByMethod(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CountByMethod() = %v, want %v", got, want)
	}
}

func TestStatusInfoTable(t *testing.T) {
	info := &StatusInfo{RPCs: map[time.Duration]MethodList{
		0:           {{name: "b2_list_buckets"}, {name: "b2_list_buckets"}},
		time.Minute: {{name: "b2_list_buckets"}, {name: "b2_upload_file"}},
	}}
	want := map[string]map[string]int{
		"b2_list_buckets": {"all time": 2, "1m0s": 1},
		"b2_upload_file":  {"1m0s": 1},
	}
	if got := info.table(); !reflect.DeepEqual(got, want) {
		t.Fatalf("table() = %v, want %v", got, want)
	}
}

func TestStatusTemplateFunctions(t *testing.T) {
	if got := funcMap["inc"].(func(int) int)(2); got != 3 {
		t.Errorf("inc(2) = %d, want 3", got)
	}
	if got := funcMap["lookUp"].(func(map[string]int, string) int)(map[string]int{"upload": 4}, "upload"); got != 4 {
		t.Errorf("lookUp(upload) = %d, want 4", got)
	}
	pRange := funcMap["pRange"].(func(int) string)
	for _, tc := range []struct {
		index int
		want  string
	}{{0, "0s - 1ms"}, {2, "3ms - 7ms"}} {
		if got := pRange(tc.index); got != tc.want {
			t.Errorf("pRange(%d) = %q, want %q", tc.index, got, tc.want)
		}
	}

	info := &StatusInfo{RPCs: map[time.Duration]MethodList{
		5 * time.Minute: {{name: "b2_upload_file"}},
		0:               {{name: "b2_list_buckets"}},
		time.Minute:     {{name: "b2_download_file_by_name"}, {name: "b2_list_buckets"}},
	}}
	wantMethods := []string{"b2_download_file_by_name", "b2_list_buckets", "b2_upload_file"}
	if got := funcMap["methods"].(func(*StatusInfo) []string)(info); !reflect.DeepEqual(got, wantMethods) {
		t.Errorf("methods() = %v, want %v", got, wantMethods)
	}
	wantDurations := []string{"all time", "1m0s", "5m0s"}
	if got := funcMap["durations"].(func(*StatusInfo) []string)(info); !reflect.DeepEqual(got, wantDurations) {
		t.Errorf("durations() = %v, want %v", got, wantDurations)
	}
}

func TestClientServeHTTPStatus(t *testing.T) {
	counter := newMethodCounter(0, 0)
	counter.record(method{name: "b2_list_buckets", status: http.StatusOK})
	client := &Client{sMethods: []methodCounter{counter}}
	response := httptest.NewRecorder()
	client.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200", response.Code)
	}
	for _, want := range []string{"b2 client status", "b2_list_buckets", "<td>1</td>"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("ServeHTTP body missing %q: %s", want, response.Body.String())
		}
	}
}
