package channel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"gpt-load/internal/models"
)

func (ch *GenericChannel) TransformRequest(req *http.Request, bodyBytes []byte, _ *models.Group) ([]byte, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("request URL is required for generic transformation")
	}

	switch {
	case strings.HasSuffix(req.URL.Path, "/v1/audio/speech"):
		return ch.transformOpenAISpeech(req, bodyBytes)
	case strings.HasSuffix(req.URL.Path, "/v1/audio/transcriptions"):
		return ch.transformOpenAITranscription(req, bodyBytes)
	default:
		return bodyBytes, nil
	}
}

func (ch *GenericChannel) transformOpenAISpeech(req *http.Request, bodyBytes []byte) ([]byte, error) {
	var source struct {
		Model          string   `json:"model"`
		Input          string   `json:"input"`
		Voice          string   `json:"voice"`
		ResponseFormat string   `json:"response_format"`
		Speed          *float64 `json:"speed"`
	}
	if err := json.Unmarshal(bodyBytes, &source); err != nil {
		return nil, fmt.Errorf("invalid OpenAI speech request: %w", err)
	}
	if strings.TrimSpace(source.Input) == "" {
		return nil, fmt.Errorf("OpenAI speech request requires input")
	}

	fish := map[string]any{
		"text": source.Input,
	}
	if source.Voice != "" {
		fish["reference_id"] = source.Voice
	}
	if source.ResponseFormat != "" {
		fish["format"] = source.ResponseFormat
	}
	if source.Speed != nil {
		fish["prosody"] = map[string]any{"speed": *source.Speed}
	}

	transformed, err := json.Marshal(fish)
	if err != nil {
		return nil, fmt.Errorf("encode Fish speech request: %w", err)
	}
	setModelHeader(req, source.Model)
	replacePathSuffix(req, "/v1/audio/speech", "/v1/tts")
	req.Header.Set("Content-Type", "application/json")
	return transformed, nil
}

func (ch *GenericChannel) transformOpenAITranscription(req *http.Request, bodyBytes []byte) ([]byte, error) {
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("invalid transcription content type: %w", err)
	}
	if mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, fmt.Errorf("OpenAI transcription request must be multipart/form-data")
	}

	reader := multipart.NewReader(bytes.NewReader(bodyBytes), params["boundary"])
	var transformed bytes.Buffer
	writer := multipart.NewWriter(&transformed)
	model := req.Header.Get("model")
	fileCount := 0

	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("read OpenAI transcription multipart body: %w", nextErr)
		}

		fieldName := part.FormName()
		if fieldName == "model" && part.FileName() == "" {
			value, readErr := io.ReadAll(part)
			if readErr != nil {
				_ = writer.Close()
				return nil, fmt.Errorf("read OpenAI transcription model: %w", readErr)
			}
			if model == "" {
				model = string(value)
			}
			continue
		}

		if (fieldName == "file" || fieldName == "audio") && part.FileName() != "" {
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
				"name":     "audio",
				"filename": part.FileName(),
			}))
			if contentType := part.Header.Get("Content-Type"); contentType != "" {
				header.Set("Content-Type", contentType)
			}
			outputPart, createErr := writer.CreatePart(header)
			if createErr != nil {
				_ = writer.Close()
				return nil, fmt.Errorf("create Fish audio multipart part: %w", createErr)
			}
			if _, copyErr := io.Copy(outputPart, part); copyErr != nil {
				_ = writer.Close()
				return nil, fmt.Errorf("copy OpenAI audio file: %w", copyErr)
			}
			fileCount++
			continue
		}

		value, readErr := io.ReadAll(part)
		if readErr != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("read OpenAI transcription field %q: %w", fieldName, readErr)
		}
		if writeErr := writer.WriteField(fieldName, string(value)); writeErr != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("write Fish transcription field %q: %w", fieldName, writeErr)
		}
	}

	if fileCount == 0 {
		_ = writer.Close()
		return nil, fmt.Errorf("OpenAI transcription request requires file")
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close Fish transcription multipart body: %w", err)
	}

	setModelHeader(req, model)
	replacePathSuffix(req, "/v1/audio/transcriptions", "/v1/asr")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return transformed.Bytes(), nil
}

func replacePathSuffix(req *http.Request, source, target string) {
	if req == nil || req.URL == nil || !strings.HasSuffix(req.URL.Path, source) {
		return
	}
	req.URL.Path = strings.TrimSuffix(req.URL.Path, source) + target
	req.URL.RawPath = ""
}

func setModelHeader(req *http.Request, model string) {
	if req != nil && req.Header.Get("model") == "" && model != "" {
		req.Header.Set("model", model)
	}
}
