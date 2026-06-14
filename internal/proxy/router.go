package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rahmandayub/gemini-router/internal/key"
)

type Router struct {
	mux            *http.ServeMux
	geminiHandler  *GeminiHandler
	openaiHandler  *OpenAIHandler
	baseURL        string
	pool           *key.Pool
}

func NewRouter(baseURL string, pool *key.Pool) *Router {
	r := &Router{
		mux:           http.NewServeMux(),
		geminiHandler: NewGeminiHandler(baseURL, pool),
		openaiHandler: NewOpenAIHandler(baseURL, pool),
		baseURL:       strings.TrimRight(baseURL, "/"),
		pool:          pool,
	}

	r.mux.HandleFunc("/v1/chat/completions", r.openaiHandler.ServeHTTP)
	r.mux.HandleFunc("/v1/models", r.modelsHandler)
	r.mux.HandleFunc("/v1beta/", r.geminiHandler.ServeHTTP)
	r.mux.HandleFunc("/health", r.healthHandler)

	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	if path == "/v1/chat/completions" {
		r.openaiHandler.ServeHTTP(w, req)
		return
	}

	if path == "/v1/models" {
		r.modelsHandler(w, req)
		return
	}

	if strings.HasPrefix(path, "/v1beta/") {
		r.geminiHandler.ServeHTTP(w, req)
		return
	}

	if path == "/health" {
		r.healthHandler(w, req)
		return
	}

	http.NotFound(w, req)
}

func (r *Router) modelsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	upstreamURL := r.baseURL + "/v1beta/models"

	upstreamReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}

	apiKey := r.pool.Next()
	upstreamReq.Header.Set("x-goog-api-key", apiKey)

	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, "failed to fetch models from upstream", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	var geminiResp GeminiModelsResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		http.Error(w, "failed to parse upstream response", http.StatusBadGateway)
		return
	}

	openAIResp := OpenAIModelsResponse{
		Object: "list",
		Data:   make([]OpenAIModel, 0, len(geminiResp.Models)),
	}

	for _, m := range geminiResp.Models {
		modelID := strings.TrimPrefix(m.Name, "models/")
		openAIResp.Data = append(openAIResp.Data, OpenAIModel{
			ID:      modelID,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "google",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openAIResp)
}

func (r *Router) healthHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"keys_count": r.pool.Len(),
	})
}
