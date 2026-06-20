package proxy

import (
	"encoding/json"
	"fmt"
)

type GeminiError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func parseGeminiError(body []byte) (code int, message, status string) {
	var geminiErr GeminiError
	if err := json.Unmarshal(body, &geminiErr); err == nil && geminiErr.Error.Message != "" {
		return geminiErr.Error.Code, geminiErr.Error.Message, geminiErr.Error.Status
	}
	return 500, string(body), "UNKNOWN"
}

func translateGeminiErrorToOpenAI(body []byte) map[string]interface{} {
	code, message, _ := parseGeminiError(body)
	errorType := "server_error"
	errorCode := fmt.Sprintf("http_status_%d", code)

	switch {
	case code == 400:
		errorType = "invalid_request_error"
		errorCode = "bad_request"
	case code == 401 || code == 403:
		errorType = "authentication_error"
		errorCode = "invalid_api_key"
	case code == 404:
		errorType = "invalid_request_error"
		errorCode = "model_not_found"
	case code == 429:
		errorType = "rate_limit_error"
		errorCode = "rate_limit_exceeded"
	case code >= 500:
		errorType = "server_error"
		errorCode = "internal_error"
	}

	return map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errorType,
			"code":    errorCode,
		},
	}
}

func translateGeminiErrorToAnthropic(body []byte) map[string]interface{} {
	code, message, _ := parseGeminiError(body)
	errorType := "api_error"

	switch {
	case code == 400:
		errorType = "invalid_request_error"
	case code == 401 || code == 403:
		errorType = "authentication_error"
	case code == 404:
		errorType = "not_found_error"
	case code == 429:
		errorType = "rate_limit_error"
	case code >= 500:
		errorType = "api_error"
	}

	return map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errorType,
			"message": message,
		},
	}
}

func translateGeminiErrorToResponses(body []byte) map[string]interface{} {
	code, message, _ := parseGeminiError(body)
	errorType := "server_error"
	errorCode := fmt.Sprintf("http_status_%d", code)

	switch {
	case code == 400:
		errorType = "invalid_request_error"
		errorCode = "bad_request"
	case code == 401 || code == 403:
		errorType = "authentication_error"
		errorCode = "invalid_api_key"
	case code == 404:
		errorType = "not_found_error"
		errorCode = "model_not_found"
	case code == 429:
		errorType = "rate_limit_error"
		errorCode = "rate_limit_exceeded"
	case code >= 500:
		errorType = "server_error"
		errorCode = "internal_error"
	}

	return map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errorType,
			"code":    errorCode,
		},
	}
}
