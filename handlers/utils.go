//go:build wasip1

package handlers

import (
	"net/http"
	"strings"
)

func isTVRequest(req *http.Request) bool {
	return strings.Contains(req.URL.Path, "/lxmusic/api/tv/")
}

func getSourceID(req *http.Request) string {
	if sourceID := req.URL.Query().Get("source_id"); sourceID != "" {
		return sourceID
	}
	return req.URL.Query().Get("source")
}
