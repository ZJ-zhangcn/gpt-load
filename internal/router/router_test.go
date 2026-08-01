package router

import "testing"

func TestIsBackendPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api", want: true},
		{path: "/api/keys", want: true},
		{path: "/apiary", want: false},
		{path: "/proxy", want: true},
		{path: "/proxy/default/v1/chat/completions", want: true},
		{path: "/proxy-pool", want: false},
		{path: "/proxy2", want: false},
		{path: "/keys", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isBackendPath(tt.path); got != tt.want {
				t.Fatalf("isBackendPath(%q) = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}
