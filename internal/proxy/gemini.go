package proxy

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/rahmandayub/gemini-router/internal/key"
)

type GeminiHandler struct {
	baseURL string
	pool    *key.Pool
}

func NewGeminiHandler(baseURL string, pool *key.Pool) *GeminiHandler {
	return &GeminiHandler{
		baseURL: strings.TrimRight(baseURL, "/"),
		pool:    pool,
	}
}

func (h *GeminiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/v1beta/") {
		http.NotFound(w, r)
		return
	}

	upstreamURL := h.baseURL + path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}

	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	apiKey := h.pool.Next()
	req.Header.Set("x-goog-api-key", apiKey)

	log.Printf("[proxy/gemini] %s %s -> keys_total=%d", r.Method, path, h.pool.Len())

	resp, err := UpstreamClient.Do(req)
	if err != nil {
		http.Error(w, "failed to forward request to upstream", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
