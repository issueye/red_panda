package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLocalDesktopCORSAllowsWailsProviderProfilePreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(localDesktopCORS())
	router.POST("/api/v1/provider-profiles", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/provider-profiles", nil)
	request.Header.Set("Origin", "http://wails.localhost")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://wails.localhost" {
		t.Fatalf("allow origin = %q, want Wails origin", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); !stringsContainFold(got, "content-type") {
		t.Fatalf("allow headers = %q, want Content-Type", got)
	}
}

func TestLocalDesktopCORSRejectsRemotePreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(localDesktopCORS())

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/provider-profiles", nil)
	request.Header.Set("Origin", "https://example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("remote preflight status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("remote preflight allow origin = %q, want empty", got)
	}
}

func stringsContainFold(value string, want string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), want) {
			return true
		}
	}
	return false
}
