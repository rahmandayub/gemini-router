package proxy

import (
	"encoding/json"
	"log"
	"os"
	"strings"
)

// debugEnabled controls whether verbose payload logging is emitted.
// It is initialized from the GEMINI_ROUTER_DEBUG environment variable
// and can be toggled at runtime via SetDebug. We avoid pulling in a full
// structured logger to keep the change footprint small.
var debugEnabled = strings.EqualFold(os.Getenv("GEMINI_ROUTER_DEBUG"), "1") ||
	strings.EqualFold(os.Getenv("GEMINI_ROUTER_DEBUG"), "true")

// SetDebug toggles verbose payload logging at runtime.
func SetDebug(enabled bool) { debugEnabled = enabled }

// IsDebug reports whether verbose payload logging is enabled.
func IsDebug() bool { return debugEnabled }

// logPayload marshals v and logs it at debug level, returning the bytes
// (or nil if v could not be marshaled) and any marshal error. Callers
// that do not care about the error can simply ignore both return values.
func logPayload(prefix string, v interface{}) ([]byte, error) {
	if !IsDebug() {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("%s payload marshal error: %v", prefix, err)
		return nil, err
	}
	return data, nil
}
