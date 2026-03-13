package internal

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFetchImageTags_ValidImageWithTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle Docker Hub API style requests
		if strings.Contains(r.URL.Path, "/v2/repositories/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"count": 4, "results": [{"name": "latest"}, {"name": "18.04"}, {"name": "20.04"}, {"name": "22.04"}],"next": null}`))
			return
		}
		// Handle OCI registry v2 API style requests
		if strings.Contains(r.URL.Path, "/tags/list") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"name":"ubuntu","tags":["latest","18.04","20.04","22.04"]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Parse the server URL to get host
	serverURL, err := url.Parse(server.URL)
	assert.NoError(t, err)

	// Create registry with test server
	registry := NewRegistry(serverURL.Host)

	// Use the test server host as the registry prefix
	gotTags, err := registry.FetchImageTags(serverURL.Host + "/library/ubuntu")

	assert.NoError(t, err)
	assert.Equal(t, []string{"latest", "18.04", "20.04", "22.04"}, gotTags)
}
