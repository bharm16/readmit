package networkaction

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"

	"github.com/bharm16/readmit/internal/destination"
)

// releaseHTTPResponse is the single release decision for direct-reference and
// runtime-provider transports. Opaque formatting is not a retention check:
// known credentials must be absent before either bytes or metadata escape.
func releaseHTTPResponse(response destination.HTTPResult, credentials [][]byte, extendedMetadata bool) (HTTPResponse, error) {
	for _, value := range credentials {
		if len(value) == 0 {
			continue
		}
		if sensitiveEcho(response.Body, value) {
			return HTTPResponse{}, refused
		}
		for _, values := range response.Header {
			for _, header := range values {
				if sensitiveEcho([]byte(header), value) {
					return HTTPResponse{}, refused
				}
			}
		}
	}
	headers := safeHeaders(response.Header)
	if extendedMetadata {
		headers = safeHeadersV2(response.Header)
	}
	return HTTPResponse{Status: response.Status, Body: ResponseBody{raw: response.Body}, header: headers}, nil
}

// The supported known-value encodings match the memory-only credential path.
// This deliberately does not claim general-purpose secret discovery.
func sensitiveEcho(body, value []byte) bool {
	if bytes.Contains(body, value) {
		return true
	}
	quoted, _ := json.Marshal(string(value))
	if len(quoted) > 2 && bytes.Contains(body, quoted[1:len(quoted)-1]) {
		return true
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if bytes.Contains(body, []byte(encoding.EncodeToString(value))) {
			return true
		}
	}
	return false
}
