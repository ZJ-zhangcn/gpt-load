package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStaticCacheKeepsHTMLFreshAndAssetsImmutable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(StaticCache())
	router.GET("/*path", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "no-cache, no-store, must-revalidate"},
		{path: "/index.html", want: "no-cache, no-store, must-revalidate"},
		{path: "/assets/index-hashed.js", want: "public, max-age=2592000, immutable"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			router.ServeHTTP(recorder, request)

			if got := recorder.Header().Get("Cache-Control"); got != tt.want {
				t.Fatalf("Cache-Control for %s = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
