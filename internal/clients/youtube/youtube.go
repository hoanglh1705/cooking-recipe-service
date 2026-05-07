// Package youtube wraps the YouTube Data API v3 search.list endpoint.
//
// We hit the REST endpoint directly (no Google SDK) to keep the dependency
// graph small. Daily quota for search.list is 100 units/call out of 10k units
// free per project.
package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://www.googleapis.com/youtube/v3"

type Client struct {
	APIKey string
	HTTP   *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type SearchResult struct {
	VideoID  string
	Title    string
	Channel  string
	URL      string
	Duration time.Duration // populated by FetchDetails
}

type searchListResp struct {
	Items []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			Title        string `json:"title"`
			ChannelTitle string `json:"channelTitle"`
		} `json:"snippet"`
	} `json:"items"`
}

// SearchTopVideos returns up to `max` matching videos for the given query.
// Returns an empty slice (not an error) when no API key is configured.
func (c *Client) SearchTopVideos(ctx context.Context, query string, max int) ([]SearchResult, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	if max <= 0 {
		max = 5
	}

	u, _ := url.Parse(apiBase + "/search")
	q := u.Query()
	q.Set("part", "snippet")
	q.Set("q", query)
	q.Set("type", "video")
	q.Set("relevanceLanguage", "vi")
	q.Set("maxResults", strconv.Itoa(max))
	q.Set("videoEmbeddable", "true")
	q.Set("key", c.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtube search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube search: status %d", resp.StatusCode)
	}
	var body searchListResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	out := make([]SearchResult, 0, len(body.Items))
	for _, it := range body.Items {
		if it.ID.VideoID == "" {
			continue
		}
		out = append(out, SearchResult{
			VideoID: it.ID.VideoID,
			Title:   it.Snippet.Title,
			Channel: it.Snippet.ChannelTitle,
			URL:     "https://www.youtube.com/watch?v=" + it.ID.VideoID,
		})
	}
	return out, nil
}

type videoListResp struct {
	Items []struct {
		ID             string `json:"id"`
		ContentDetails struct {
			Duration string `json:"duration"` // ISO8601 e.g. PT5M30S
		} `json:"contentDetails"`
	} `json:"items"`
}

// FetchDurations enriches the given results with content-details duration.
// Quiet on auth failure so MVP runs without API key.
func (c *Client) FetchDurations(ctx context.Context, results []SearchResult) ([]SearchResult, error) {
	if c.APIKey == "" || len(results) == 0 {
		return results, nil
	}
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.VideoID
	}

	u, _ := url.Parse(apiBase + "/videos")
	q := u.Query()
	q.Set("part", "contentDetails")
	q.Set("id", strings.Join(ids, ","))
	q.Set("key", c.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return results, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return results, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return results, nil
	}
	var body videoListResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return results, err
	}
	durByID := map[string]time.Duration{}
	for _, it := range body.Items {
		durByID[it.ID] = parseISO8601Duration(it.ContentDetails.Duration)
	}
	for i, r := range results {
		if d, ok := durByID[r.VideoID]; ok {
			results[i].Duration = d
		}
	}
	return results, nil
}

// parseISO8601Duration is a tiny, dependency-free parser for the "PT#H#M#S"
// strings that YouTube returns. Returns 0 if unrecognized — callers treat
// that as "unknown" rather than erroring.
func parseISO8601Duration(s string) time.Duration {
	if !strings.HasPrefix(s, "PT") {
		return 0
	}
	s = strings.TrimPrefix(s, "PT")
	var (
		total time.Duration
		num   strings.Builder
	)
	for _, ch := range s {
		switch {
		case ch >= '0' && ch <= '9':
			num.WriteRune(ch)
		case ch == 'H':
			n, _ := strconv.Atoi(num.String())
			total += time.Duration(n) * time.Hour
			num.Reset()
		case ch == 'M':
			n, _ := strconv.Atoi(num.String())
			total += time.Duration(n) * time.Minute
			num.Reset()
		case ch == 'S':
			n, _ := strconv.Atoi(num.String())
			total += time.Duration(n) * time.Second
			num.Reset()
		}
	}
	return total
}
