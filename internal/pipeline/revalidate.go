package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// RevalidateNext returns a PublishHook that POSTs to a Next.js revalidate
// endpoint after a recipe is published. Returns nil if the URL is empty.
func RevalidateNext(endpoint, secret string) PublishHook {
	if endpoint == "" {
		return nil
	}
	httpc := &http.Client{Timeout: 10 * time.Second}

	return func(ctx context.Context, slug string) error {
		u, err := url.Parse(endpoint)
		if err != nil {
			return err
		}
		q := u.Query()
		q.Set("path", "/cong-thuc/"+slug)
		if secret != "" {
			q.Set("secret", secret)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
		if err != nil {
			return err
		}
		resp, err := httpc.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("revalidate: status %d", resp.StatusCode)
		}
		return nil
	}
}
