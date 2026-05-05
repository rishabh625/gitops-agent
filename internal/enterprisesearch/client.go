// Package enterprisesearch calls Vertex AI Search (Discovery Engine) serving endpoints
// to retrieve organizational knowledge when GitOps automation steps fail.
package enterprisesearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const defaultTimeZone = "Asia/Calcutta"

// Client issues :search requests to a Discovery Engine serving config (e.g. Gemini Enterprise app).
type Client struct {
	log         *slog.Logger
	http        *http.Client
	endpoint    string
	session     string
	timeZone    string
	maxBody     int
}

// NewFromEnv returns a client when DISCOVERY_ENGINE_SEARCH_ENABLED is true and required
// variables are set. Returns (nil, nil) when the feature is disabled.
//
// Environment:
//   - DISCOVERY_ENGINE_SEARCH_ENABLED — set to "1" or "true" to enable
//   - DISCOVERY_ENGINE_LOCATION — region for API host, e.g. "eu" -> eu-discoveryengine.googleapis.com
//   - DISCOVERY_ENGINE_SERVING_CONFIG — resource name up to servingConfigs/default_search, e.g.
//     projects/123/locations/eu/collections/default_collection/engines/my-engine/servingConfigs/default_search
//   - DISCOVERY_ENGINE_SESSION (optional) — full session resource name; if empty, derived as
//     .../engines/<id>/sessions/- from the serving config path
//   - DISCOVERY_ENGINE_TIME_ZONE (optional) — defaults to Asia/Calcutta
//
// Git MCP fallback (read by executor, not this package): DISCOVERY_ENGINE_GIT_FALLBACK — when "true"
// (default if unset), a failed Git MCP step triggers an enterprise search with an action-oriented query;
// if the response text contains a PR/MR URL, the step is treated as successful.
func NewFromEnv(log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.Default()
	}
	en := strings.ToLower(strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_SEARCH_ENABLED")))
	if en != "1" && en != "true" && en != "yes" {
		return nil, nil
	}
	loc := strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_LOCATION"))
	serving := strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_SERVING_CONFIG"))
	if loc == "" || serving == "" {
		return nil, fmt.Errorf("enterprisesearch: DISCOVERY_ENGINE_LOCATION and DISCOVERY_ENGINE_SERVING_CONFIG are required when search is enabled")
	}
	session := strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_SESSION"))
	if session == "" {
		session = defaultSessionFromServingConfig(serving)
	}
	tz := strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_TIME_ZONE"))
	if tz == "" {
		tz = defaultTimeZone
	}
	host := fmt.Sprintf("https://%s-discoveryengine.googleapis.com", loc)
	// v1alpha .../servingConfigs/NAME:search
	endpoint := fmt.Sprintf("%s/v1alpha/%s:search", host, strings.TrimLeft(serving, "/"))

	ctx := context.Background()
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("enterprisesearch: default credentials: %w", err)
	}
	return &Client{
		log:      log,
		http:     oauth2.NewClient(ctx, ts),
		endpoint: endpoint,
		session:  session,
		timeZone: tz,
		maxBody:  1 << 20, // 1 MiB
	}, nil
}

func defaultSessionFromServingConfig(serving string) string {
	// .../engines/<engineId>/servingConfigs/default_search -> .../engines/<engineId>/sessions/-
	const marker = "/servingConfigs/"
	i := strings.Index(serving, marker)
	if i < 0 {
		return ""
	}
	return serving[:i] + "/sessions/-"
}

// Search runs a natural-language query against the configured app and returns a short text summary
// of the top matching snippets (for operator-facing hints).
func (c *Client) Search(ctx context.Context, query string) (string, error) {
	if c == nil {
		return "", nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return "", nil
	}
	body := map[string]any{
		"query":     q,
		"pageSize":  10,
		"session":   c.session,
		"spellCorrectionSpec": map[string]any{
			"mode": "AUTO",
		},
		"languageCode": "en-US",
		"relevanceScoreSpec": map[string]any{
			"returnRelevanceScore": true,
		},
		"userInfo": map[string]any{
			"timeZone": c.timeZone,
		},
		"contentSearchSpec": map[string]any{
			"snippetSpec": map[string]any{
				"returnSnippet": true,
			},
		},
		"naturalLanguageQueryUnderstandingSpec": map[string]any{
			"filterExtractionCondition": "ENABLED",
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("enterprisesearch: request: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(c.maxBody)))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.log.Warn("enterprisesearch non-success", "status", resp.StatusCode, "body", truncate(string(b), 500))
		return "", fmt.Errorf("enterprisesearch: HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	return extractSummary(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// extractSummary pulls human-readable lines from a Discovery Engine search response.
func extractSummary(raw []byte) (string, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return "", err
	}
	results, _ := root["results"].([]any)
	var parts []string
	for i, r := range results {
		if i >= 5 {
			break
		}
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		line := resultLine(m)
		if line != "" {
			parts = append(parts, fmt.Sprintf("• %s", line))
		}
	}
	if len(parts) == 0 {
		if sm, ok := root["summary"].(map[string]any); ok && sm != nil {
			if t, ok := sm["summaryText"].(string); ok && t != "" {
				return t, nil
			}
		}
		if s, ok := root["summary"].(string); ok && s != "" {
			return s, nil
		}
		return "", nil
	}
	return strings.Join(parts, "\n"), nil
}

func resultLine(m map[string]any) string {
	if doc, ok := m["document"].(map[string]any); ok {
		if s := joinSnippets(doc); s != "" {
			return s
		}
	}
	if ch, ok := m["chunk"].(map[string]any); ok {
		if c, _ := ch["content"].(string); c != "" {
			return strings.TrimSpace(c)
		}
	}
	// Unstructured fallbacks
	if s, _ := m["snippet"].(string); s != "" {
		return strings.TrimSpace(s)
	}
	return ""
}

func joinSnippets(doc map[string]any) string {
	// jsonData from ingested content
	if js, ok := doc["jsonData"].(string); ok && js != "" {
		return truncate(strings.TrimSpace(js), 500)
	}
	if d, ok := doc["derivedStructData"].(map[string]any); ok {
		if b, err := json.Marshal(d); err == nil {
			return truncate(strings.TrimSpace(string(b)), 500)
		}
	}
	if snips, ok := doc["snippets"].([]any); ok {
		var out []string
		for _, s := range snips {
			sm, _ := s.(map[string]any)
			if sm == nil {
				continue
			}
			if t, _ := sm["snippet"].(string); t != "" {
				out = append(out, strings.TrimSpace(t))
			}
		}
		if len(out) > 0 {
			return strings.Join(out, " ")
		}
	}
	if s, _ := doc["name"].(string); s != "" {
		return s
	}
	return ""
}
