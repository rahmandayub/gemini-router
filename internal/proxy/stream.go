package proxy

import (
	"bufio"
	"io"
	"net/http"
)

func PipeStreamingResponse(w http.ResponseWriter, resp *http.Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, resp.Body)
		return
	}

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		w.Write([]byte(line + "\n\n"))
		flusher.Flush()
	}
}

func WriteSSE(w http.ResponseWriter, data []byte) {
	w.Write([]byte("data: "))
	w.Write(data)
	w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
