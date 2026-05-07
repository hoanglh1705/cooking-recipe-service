// Package tiktok is a placeholder. TikTok has no public content API for
// indie developers and aggressive scraping protection. The MVP keeps the
// package as a typed stub so callers can switch implementations later
// (yt-dlp + tiktok URL, third-party API, or manual paste).
package tiktok

import "context"

type SearchResult struct {
	VideoID string
	URL     string
	Title   string
	Author  string
}

type Client struct{}

func New() *Client { return &Client{} }

// SearchTopVideos always returns nil — implement when scoping allows.
func (c *Client) SearchTopVideos(_ context.Context, _ string, _ int) ([]SearchResult, error) {
	return nil, nil
}
