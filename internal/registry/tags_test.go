package registry

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTags(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v2/repositories/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"count": 4, "results": [{"name": "latest"}, {"name": "18.04"}, {"name": "20.04"}, {"name": "22.04"}],"next": null}`))
			return
		}
		if strings.Contains(r.URL.Path, "/tags/list") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"ubuntu","tags":["latest","18.04","20.04","22.04"]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	assert.NoError(t, err)

	client := New(serverURL.Host)

	gotTags, err := client.Tags(serverURL.Host + "/library/ubuntu")

	assert.NoError(t, err)
	assert.Equal(t, []string{"latest", "18.04", "20.04", "22.04"}, gotTags)
}
