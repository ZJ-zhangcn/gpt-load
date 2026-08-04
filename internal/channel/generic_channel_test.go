package channel

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"gpt-load/internal/models"
	"gpt-load/internal/utils"

	"github.com/gin-gonic/gin"
)

func TestGenericValidationEndpointDefaultsToFishModelList(t *testing.T) {
	if got := utils.GetValidationEndpoint(&models.Group{ChannelType: "generic"}); got != "/model" {
		t.Fatalf("generic validation endpoint = %q, want /model", got)
	}
}

func TestGenericChannelBuildsNativeAudioPaths(t *testing.T) {
	upstream, err := url.Parse("https://api.example.test/base")
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}

	ch := &GenericChannel{BaseChannel: &BaseChannel{
		Name:      "generic",
		Upstreams: []UpstreamInfo{{URL: upstream, Weight: 1}},
	}}

	for _, path := range []string{"/v1/tts", "/v1/asr"} {
		t.Run(path, func(t *testing.T) {
			originalURL, err := url.Parse("/proxy/fish-audio" + path + "?trace=1")
			if err != nil {
				t.Fatalf("parse original URL: %v", err)
			}

			got, err := ch.BuildUpstreamURL(originalURL, "fish-audio")
			if err != nil {
				t.Fatalf("build upstream URL: %v", err)
			}

			want := "https://api.example.test/base" + path + "?trace=1"
			if got != want {
				t.Fatalf("upstream URL = %q, want %q", got, want)
			}
		})
	}
}

func TestGenericChannelPreservesNativeAudioRequestAndInjectsKey(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]struct {
		method        string
		authorization string
		model         string
		contentType   string
		body          string
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mu.Lock()
		seen[r.URL.Path] = struct {
			method        string
			authorization string
			model         string
			contentType   string
			body          string
		}{
			method:        r.Method,
			authorization: r.Header.Get("Authorization"),
			model:         r.Header.Get("model"),
			contentType:   r.Header.Get("Content-Type"),
			body:          string(body),
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("binary-audio"))
	}))
	defer upstream.Close()

	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	key := &models.APIKey{KeyValue: "fish-key"}
	payloads := map[string]string{
		"/v1/tts": `{"text":"hello","reference_id":"voice-1","format":"mp3"}`,
		"/v1/asr": "native-audio-body",
	}

	for path, payload := range payloads {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, upstream.URL+path, strings.NewReader(payload))
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer client-key")
			contentType := "application/json"
			if path == "/v1/asr" {
				contentType = "multipart/form-data; boundary=fixture"
			}
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("model", "s2-pro")
			ch.ModifyRequest(req, key, &models.Group{})

			resp, err := upstream.Client().Do(req)
			if err != nil {
				t.Fatalf("send request: %v", err)
			}
			defer resp.Body.Close()
			responseBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if got := string(responseBody); got != "binary-audio" {
				t.Fatalf("response body = %q, want binary response", got)
			}

			mu.Lock()
			request := seen[path]
			mu.Unlock()
			if request.method != http.MethodPost {
				t.Fatalf("method = %q, want POST", request.method)
			}
			if request.authorization != "Bearer fish-key" {
				t.Fatalf("authorization = %q, want injected key", request.authorization)
			}
			if request.model != "s2-pro" {
				t.Fatalf("model header = %q, want preserved header", request.model)
			}
			if request.contentType != contentType {
				t.Fatalf("content type = %q, want %s", request.contentType, contentType)
			}
			if request.body != payload {
				t.Fatalf("request body = %q, want exact payload %q", request.body, payload)
			}
		})
	}
}

func TestGenericChannelValidationUsesConfiguredGETEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("validation method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/model" {
			t.Errorf("validation path = %s, want /model", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fish-key" {
			t.Errorf("validation authorization = %q, want injected key", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	parsedURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	ch := &GenericChannel{BaseChannel: &BaseChannel{
		Name:               "generic",
		Upstreams:          []UpstreamInfo{{URL: parsedURL, Weight: 1}},
		ValidationEndpoint: "/model",
		HTTPClient:         upstream.Client(),
	}}

	valid, err := ch.ValidateKey(context.Background(), &models.APIKey{KeyValue: "fish-key"}, &models.Group{})
	if err != nil {
		t.Fatalf("validate key: %v", err)
	}
	if !valid {
		t.Fatal("expected 204 validation response to be valid")
	}
}

func TestGenericChannelExtractsFishModelHeaderAndStreamSignals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tts", bytes.NewReader(nil))
	ctx.Request.Header.Set("model", "s2-pro")

	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	if got := ch.ExtractModel(ctx, []byte(`{"text":"hello"}`)); got != "s2-pro" {
		t.Fatalf("model = %q, want header model", got)
	}
	if ch.IsStreamRequest(ctx, []byte(`{"text":"hello"}`)) {
		t.Fatal("binary TTS request should not be treated as a stream")
	}

	ctx.Request.Header.Set("Accept", "text/event-stream")
	if !ch.IsStreamRequest(ctx, nil) {
		t.Fatal("event-stream request should be treated as a stream")
	}
}
