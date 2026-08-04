package channel

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gpt-load/internal/models"
)

func TestGenericChannelTransformsOpenAISpeechRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://upstream.example.test/base/v1/audio/speech?trace=1", strings.NewReader(`{"model":"s2-pro","input":"hello","voice":"voice-1","response_format":"mp3","speed":1.25}`))
	req.Header.Set("Content-Type", "application/json")

	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}

	transformed, err := ch.TransformRequest(req, body, &models.Group{})
	if err != nil {
		t.Fatalf("transform speech request: %v", err)
	}

	if got, want := req.URL.Path, "/base/v1/tts"; got != want {
		t.Fatalf("upstream path = %q, want %q", got, want)
	}
	if got, want := req.URL.RawQuery, "trace=1"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("model"), "s2-pro"; got != want {
		t.Fatalf("model header = %q, want %q", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(transformed, &payload); err != nil {
		t.Fatalf("decode transformed speech body: %v", err)
	}
	assertStringField(t, payload, "text", "hello")
	assertStringField(t, payload, "reference_id", "voice-1")
	assertStringField(t, payload, "format", "mp3")
	if _, ok := payload["model"]; ok {
		t.Fatal("transformed speech body still contains model")
	}
	if _, ok := payload["input"]; ok {
		t.Fatal("transformed speech body still contains input")
	}
	if _, ok := payload["voice"]; ok {
		t.Fatal("transformed speech body still contains voice")
	}

	prosody, ok := payload["prosody"].(map[string]any)
	if !ok {
		t.Fatalf("prosody = %#v, want object", payload["prosody"])
	}
	if got, want := prosody["speed"], 1.25; got != want {
		t.Fatalf("prosody.speed = %#v, want %#v", got, want)
	}
}

func TestGenericChannelTransformsOpenAITranscriptionMultipart(t *testing.T) {
	var original bytes.Buffer
	writer := multipart.NewWriter(&original)
	if err := writer.WriteField("model", "s2-pro"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("language", "zh"); err != nil {
		t.Fatalf("write language field: %v", err)
	}
	filePart, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := filePart.Write([]byte("audio-bytes")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://upstream.example.test/base/v1/audio/transcriptions", bytes.NewReader(original.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	transformed, err := ch.TransformRequest(req, original.Bytes(), &models.Group{})
	if err != nil {
		t.Fatalf("transform transcription request: %v", err)
	}

	if got, want := req.URL.Path, "/base/v1/asr"; got != want {
		t.Fatalf("upstream path = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("model"), "s2-pro"; got != want {
		t.Fatalf("model header = %q, want %q", got, want)
	}

	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse transformed content type: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q, want multipart/form-data", mediaType)
	}
	reader := multipart.NewReader(bytes.NewReader(transformed), params["boundary"])
	var gotLanguage, gotFileName, gotFileBody string
	var fileCount, modelCount int
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read transformed multipart: %v", err)
		}
		partBody, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read transformed part: %v", err)
		}
		switch part.FormName() {
		case "model":
			modelCount++
		case "language":
			gotLanguage = string(partBody)
		case "audio":
			fileCount++
			gotFileName = part.FileName()
			gotFileBody = string(partBody)
		case "file":
			t.Fatal("transformed transcription still contains file field")
		}
	}
	if modelCount != 0 {
		t.Fatalf("model field count = %d, want 0", modelCount)
	}
	if gotLanguage != "zh" {
		t.Fatalf("language = %q, want zh", gotLanguage)
	}
	if fileCount != 1 || gotFileName != "sample.wav" || gotFileBody != "audio-bytes" {
		t.Fatalf("audio part = count %d, filename %q, body %q", fileCount, gotFileName, gotFileBody)
	}
}

func TestGenericChannelLeavesNativeAudioRequestUnchanged(t *testing.T) {
	body := []byte(`{"text":"hello","reference_id":"voice-1","format":"mp3"}`)
	req := httptest.NewRequest(http.MethodPost, "https://upstream.example.test/base/v1/tts?trace=1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("model", "s2-pro")

	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	transformed, err := ch.TransformRequest(req, body, &models.Group{})
	if err != nil {
		t.Fatalf("transform native request: %v", err)
	}
	if got, want := req.URL.Path, "/base/v1/tts"; got != want {
		t.Fatalf("native path = %q, want %q", got, want)
	}
	if !bytes.Equal(transformed, body) {
		t.Fatalf("native body changed from %q to %q", body, transformed)
	}
	if got, want := req.Header.Get("model"), "s2-pro"; got != want {
		t.Fatalf("native model header = %q, want %q", got, want)
	}
}

func TestGenericChannelRejectsInvalidOpenAISpeechRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://upstream.example.test/v1/audio/speech", strings.NewReader(`{"model":"s2-pro"}`))
	req.Header.Set("Content-Type", "application/json")

	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	_, err := ch.TransformRequest(req, []byte(`{"model":"s2-pro"}`), &models.Group{})
	if err == nil {
		t.Fatal("invalid speech request unexpectedly transformed")
	}
	if !strings.Contains(err.Error(), "input") {
		t.Fatalf("error = %q, want input validation detail", err)
	}
}

func TestGenericChannelRejectsTranscriptionWithoutFile(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "s2-pro"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://upstream.example.test/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ch := &GenericChannel{BaseChannel: &BaseChannel{Name: "generic"}}
	_, err := ch.TransformRequest(req, body.Bytes(), &models.Group{})
	if err == nil {
		t.Fatal("transcription without file unexpectedly transformed")
	}
	if !strings.Contains(err.Error(), "file") {
		t.Fatalf("error = %q, want file validation detail", err)
	}
}

func assertStringField(t *testing.T, payload map[string]any, name, want string) {
	t.Helper()
	got, ok := payload[name].(string)
	if !ok || got != want {
		t.Fatalf("%s = %#v, want %q", name, payload[name], want)
	}
}
