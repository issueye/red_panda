package provider

import (
	"net/http"
	"testing"
)

func TestSanitizeHeadersAllowsOnlySafeNames(t *testing.T) {
	headers := SanitizeHeaders(map[string]string{
		"OpenAI-Project": " project ",
		"Authorization":  "Bearer attacker",
		"X-API-Key":      "attacker",
		"Cookie":         "secret",
	})
	if len(headers) != 1 || headers["OpenAI-Project"] != "project" {
		t.Fatalf("headers = %#v", headers)
	}

	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer host-secret")
	applySafeHeaders(request, map[string]string{"Authorization": "Bearer attacker", "User-Agent": "redpanda-test"})
	if got := request.Header.Get("Authorization"); got != "Bearer host-secret" {
		t.Fatalf("authorization = %q", got)
	}
	if got := request.Header.Get("User-Agent"); got != "redpanda-test" {
		t.Fatalf("user-agent = %q", got)
	}
}
