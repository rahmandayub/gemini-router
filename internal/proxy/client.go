package proxy

import (
	"net/http"
	"time"
)

var UpstreamClient = &http.Client{Timeout: 60 * time.Second}
