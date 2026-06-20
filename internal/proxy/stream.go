package proxy

import "net/http"

func WriteSSEEvent(w http.ResponseWriter, eventType string, data []byte) {
	w.Write([]byte("event: "))
	w.Write([]byte(eventType))
	w.Write([]byte("\ndata: "))
	w.Write(data)
	w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
