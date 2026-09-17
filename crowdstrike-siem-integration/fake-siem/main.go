// Command fake-siem is a throwaway HTTP server that mimics a CrowdStrike SIEM
// HEC (HTTP Event Collector) endpoint, so sync.py can be tested without real
// CrowdStrike access. Point CROWDSTRIKE_HEC_URL at this server, then swap it for
// the real HEC URL later with no code change.
//
// It accepts POST /services/collector with an "Authorization: Bearer <token>"
// header, counts the newline-delimited HEC records in the body, logs each
// package name, and returns the HEC success body {"text":"Success","code":0}.
// A missing or wrong token returns {"text":"Invalid token","code":4} with 403.
//
// Run:
//
//	FAKE_HEC_TOKEN=test-token go run ./crowdstrike-siem-integration/fake-siem
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	token := envOr("FAKE_HEC_TOKEN", "test-token")
	addr := envOr("FAKE_HEC_ADDR", ":8088")

	mux := http.NewServeMux()
	mux.HandleFunc("/services/collector", collectorHandler(token))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("fake CrowdStrike HEC listening on %s (POST /services/collector)", addr)
	log.Fatal(srv.ListenAndServe())
}

func collectorHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			reply(w, http.StatusForbidden, "Invalid token", 4)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			reply(w, http.StatusBadRequest, "Bad request", 6)
			return
		}

		count := 0
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			count++
			log.Printf("received event: %s", packageName(line))
		}
		log.Printf("batch accepted: %d event(s)", count)
		reply(w, http.StatusOK, "Success", 0)
	}
}

// reply writes an HEC-style JSON response.
func reply(w http.ResponseWriter, status int, text string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"text": text, "code": code}); err != nil {
		log.Printf("write response: %v", err)
	}
}

// packageName digs the package name out of one HEC record for a readable log.
func packageName(line string) string {
	var record struct {
		Event map[string]any `json:"event"`
	}
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return "?"
	}
	pv := mapAt(mapAt(mapAt(record.Event, "pmgEvent"), "packageDecision"), "packageVersion")
	if pkg := mapAt(pv, "package"); pkg != nil {
		if name, ok := pkg["name"].(string); ok {
			return name
		}
	}
	return "?"
}

func mapAt(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" { //nolint:forbidigo // throwaway test server, not production config
		return v
	}
	return fallback
}
