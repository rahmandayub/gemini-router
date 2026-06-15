package proxy

import (
	"net/http"
)

var UpstreamClient = &http.Client{}
