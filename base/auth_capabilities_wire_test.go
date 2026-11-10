package base

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Storage capabilities belong to allowed in the v4 response:
// https://www.backblaze.com/apidocs/b2-authorize-account
func TestAuthorizeAccountPreservesNestedCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name         string
		allowed      string
		sibling      string
		capabilities []string
		buckets      []string
		prefix       string
	}{
		{
			name: "restricted nested capabilities",
			allowed: `{"buckets":[{"id":"bucket-a","name":"first"},{"id":"bucket-b","name":null}],` +
				`"capabilities":["readFiles","bypassGovernance"],"namePrefix":"prefix/"}`,
			capabilities: []string{"readFiles", "bypassGovernance"},
			buckets:      []string{"bucket-a", "bucket-b"}, prefix: "prefix/",
		},
		{
			name:         "unrestricted nested capabilities",
			allowed:      `{"buckets":null,"capabilities":["readFiles","bypassGovernance"],"namePrefix":null}`,
			capabilities: []string{"readFiles", "bypassGovernance"},
		},
		{name: "absent allowed"},
		{name: "null allowed", allowed: `null`},
		{
			name:    "absent capabilities preserve restrictions",
			allowed: `{"buckets":[{"id":"bucket-a","name":"first"}],"namePrefix":"prefix/"}`,
			buckets: []string{"bucket-a"}, prefix: "prefix/",
		},
		{name: "null capabilities", allowed: `{"buckets":null,"capabilities":null,"namePrefix":null}`},
		{name: "empty capabilities", allowed: `{"buckets":[],"capabilities":[],"namePrefix":null}`},
		{
			// Deliberately conflicting legacy data must not override allowed.
			name:         "nested capabilities override sibling",
			allowed:      `{"buckets":null,"capabilities":["readFiles","bypassGovernance"],"namePrefix":null}`,
			sibling:      `,"capabilities":["writeFiles"]`,
			capabilities: []string{"readFiles", "bypassGovernance"},
		},
		{name: "sibling alone is ignored", sibling: `,"capabilities":["writeFiles"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			allowed := ""
			if tc.allowed != "" {
				allowed = `,"allowed":` + tc.allowed
			}
			// Only endpoint URLs vary. Scope and the unrelated group's capabilities
			// are independent literals rather than values echoed from an SDK request.
			response := fmt.Sprintf(`{"accountId":"account-id","authorizationToken":"test-token","applicationKeyExpirationTimestamp":null,"apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"recommendedPartSize":100000000,"apiUrl":%[1]q,"downloadUrl":%[1]q,"s3ApiUrl":%[1]q%[2]s%[3]s},"groupsApi":{"capabilities":["all"],"groupsApiUrl":"https://groups.example","infoType":"groupsApi"}}}`, srv.URL(), allowed, tc.sibling)
			srv.StrictReplySequence("b2_authorize_account", b2wiretest.Response{Status: http.StatusOK, Body: response})
			account, err := AuthorizeAccount(context.Background(), "test-key-id", "test-key", SetAPIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			data := account.AuthData()
			if !slices.Equal(data.AllowedCapabilities, tc.capabilities) {
				t.Errorf("AllowedCapabilities = %q, want %q", data.AllowedCapabilities, tc.capabilities)
			}
			if !slices.Equal(data.AllowedBuckets, tc.buckets) || data.AllowedPrefix != tc.prefix {
				t.Errorf("restrictions = (%q, %q), want (%q, %q)", data.AllowedBuckets, data.AllowedPrefix, tc.buckets, tc.prefix)
			}
			if data.AccountID != "account-id" || data.AuthToken != "test-token" ||
				data.APIURL != srv.URL() || data.DownloadURL != srv.URL() || data.S3URL != srv.URL() ||
				data.AbsMinPartSize != 5_000_000 || data.RecommendedPartSize != 100_000_000 {
				t.Error("authorization changed fields outside the allowed scope")
			}
			request := srv.Last(t, "b2_authorize_account")
			if request.Method != http.MethodGet || request.Path != "/b2api/v4/b2_authorize_account" {
				t.Errorf("authorize request = %s %s, want GET /b2api/v4/b2_authorize_account", request.Method, request.Path)
			}
		})
	}
}
