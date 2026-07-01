package sshghid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (a *App) refreshUserCache(ctx context.Context, username string) error {
	keys, err := a.fetchKeys(ctx, username)
	if err != nil {
		_ = a.logf("refresh %s failed: %v", username, err)
		return err
	}
	cache := UserCache{Username: username, Keys: keys, FetchedAt: a.Now().UTC()}
	if err := a.saveCache(cache); err != nil {
		return err
	}
	_ = a.logf("refreshed %s (%d keys)", username, len(keys))
	return nil
}

func (a *App) fetchKeys(ctx context.Context, username string) ([]string, error) {
	url := fmt.Sprintf("%s/%s.keys", a.BaseURL, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", httpUserAgent())
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("fetch %s: unexpected status %s %s", username, resp.Status, strings.TrimSpace(string(body)))
	}
	lines, err := readNonEmptyLines(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s keys: %w", username, err)
	}
	keys, err := validateAndNormalizePublicKeyLines("github keys for "+username, lines)
	if err != nil {
		return nil, err
	}
	return keys, nil
}
