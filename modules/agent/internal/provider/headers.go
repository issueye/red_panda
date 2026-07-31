package provider

import (
	"net/http"
	"strings"
)

const providerUserAgent = "red_panda/2.0"

var safeProviderHeaders = map[string]string{
	"anthropic-beta":      "Anthropic-Beta",
	"openai-organization": "OpenAI-Organization",
	"openai-project":      "OpenAI-Project",
	"x-request-id":        "X-Request-ID",
}

// SanitizeHeaders keeps only non-sensitive headers explicitly supported by the host.
func SanitizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for name, value := range headers {
		canonical, ok := safeProviderHeaders[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			continue
		}
		result[canonical] = strings.TrimSpace(value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func applySafeHeaders(request *http.Request, headers map[string]string) {
	for name, value := range SanitizeHeaders(headers) {
		request.Header.Set(name, value)
	}
	request.Header.Set("User-Agent", providerUserAgent)
}
