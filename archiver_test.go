package obelisk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

//func servefiles() {
//}

func TestArchiver_Validate(t *testing.T) {
	arc := &Archiver{
		Cache:                 nil,
		UserAgent:             "",
		MaxConcurrentDownload: 0,
		isValidated:           false,
		dlSemaphore:           nil,
		Transport:             nil,
		RequestTimeout:        0,
		httpClient:            nil,
	}

	arc.Validate()

	if arc.Cache == nil {
		t.Error("Cache should not be nil")
	}

	if arc.UserAgent == "" {
		t.Error("UserAgent should not be empty")
	}

	if arc.MaxConcurrentDownload <= 0 {
		t.Error("MaxConcurrentDownload should be greater than 0")
	}

	if !arc.isValidated {
		t.Error("isValidated should be true")
	}

	if arc.dlSemaphore == nil {
		t.Error("dlSemaphore should not be nil")
	}

	if arc.Transport == nil {
		t.Error("Transport should not be nil")
	}

	if arc.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}

func TestArchiver_Archive(t *testing.T) {
	fs := http.FileServer(http.Dir("./testdata/"))

	// start a test server with the file server handler
	server := httptest.NewServer(fs)

	defer server.Close()

	archiver := &Archiver{
		Cache:                 nil,
		UserAgent:             "",
		MaxConcurrentDownload: 0,
		isValidated:           true,
		dlSemaphore:           nil,
		Transport:             nil,
		RequestTimeout:        0,
		httpClient:            &http.Client{},
	}

	// Create a mock request
	req := Request{
		URL: server.URL,
	}

	// Call the Archive method and capture the result
	result, contentType, err := archiver.Archive(context.Background(), req)

	// Check if there was an error
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check if the result is not empty
	if len(result) == 0 {
		t.Errorf("Empty result")
	}

	// Check if the content type is not empty
	if contentType == "" {
		t.Errorf("Empty content type")
	}

	t.Run("Test isvalidURL", func(t *testing.T) {
		archiver.isValidated = false
		result, contentType, err = archiver.Archive(context.Background(), req)
		assert.Equal(t, []byte(nil), result)
		assert.Equal(t, "", contentType)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "archiver hasn't been validated")
	})
	t.Run("Test url is empty", func(t *testing.T) {
		archiver.isValidated = true
		req.URL = ""
		result, contentType, err = archiver.Archive(context.Background(), req)
		assert.Equal(t, []byte(nil), result)
		assert.Equal(t, "", contentType)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request url is not specified")
	})
	t.Run("Test not valid url ", func(t *testing.T) {
		req.URL = "notValidURL"
		result, contentType, err = archiver.Archive(context.Background(), req)
		assert.Equal(t, []byte(nil), result)
		assert.Equal(t, "", contentType)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "url \"notValidURL\" is not valid")
	})
}

func TestTransform(t *testing.T) {
	// Create a new instance of the Archiver struct
	//arc := &Archiver{}

	arc := &Archiver{
		Cache:                 nil,
		UserAgent:             "",
		MaxConcurrentDownload: 0,
		isValidated:           true,
		dlSemaphore:           nil,
		Transport:             nil,
		RequestTimeout:        0,
		httpClient:            &http.Client{},
	}
	// Test case 1: No WrapDirectory specified
	uri := "https://raw.githubusercontent.com/go-shiori/obelisk/master/docs/readme/logo.png"
	content := []byte("image content")
	contentType := "image/jpeg"

	result := arc.transform(uri, content, contentType)
	expected := createDataURL(content, contentType)

	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}

	// Test case 2: WrapDirectory specified
	arc.WrapDirectory = "/path/to/directory"

	result = arc.transform(uri, content, contentType)
	expected = "data:image/jpeg;base64,aW1hZ2UgY29udGVudA=="

	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}

func TestStore(t *testing.T) {
	arc := &Archiver{
		WrapDirectory: "/tmp/some",
	}

	// Test case 1: Empty URI
	path, rel, err := arc.store("")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if path != "" || rel != "" {
		t.Errorf("Expected empty path and rel, got path: %s, rel: %s", path, rel)
	}

	// Test case 2: Valid URI
	uri := "http://example.com/statics/css/foo.css"
	path, rel, err = arc.store(uri)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	expectedPath := "/tmp/some/statics/css/foo.css"
	expectedRel := "/statics/css/foo.css"
	if path != expectedPath || rel != expectedRel {
		t.Errorf("Expected path: %s, rel: %s, got path: %s, rel: %s", expectedPath, expectedRel, path, rel)
	}

	// Test case 3: Invalid URI
	uri = "invalid uri"
	path, rel, err = arc.store(uri)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if path != "" || rel != "" {
		t.Errorf("Expected empty path and rel, got path: %s, rel: %s", path, rel)
	}

	// Test case 4: Error creating directory
	arc.WrapDirectory = "/nonexistent"
	uri = "http://example.com/statics/css/foo.css"
	path, rel, err = arc.store(uri)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if path != "" || rel != "" {
		t.Errorf("Expected empty path and rel, got path: %s, rel: %s", path, rel)
	}
}
