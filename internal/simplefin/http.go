package simplefin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPClient struct {
	http *http.Client
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *HTTPClient) Claim(ctx context.Context, setupToken string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(setupToken))
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(setupToken))
		if err != nil {
			return "", fmt.Errorf("setup token is not valid base64")
		}
	}
	claimURL := string(raw)
	if _, err := url.ParseRequestURI(claimURL); err != nil {
		return "", fmt.Errorf("setup token does not contain a URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Length", "0")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claim failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	access := strings.TrimSpace(string(body))
	if _, err := url.ParseRequestURI(access); err != nil {
		return "", fmt.Errorf("claim returned an invalid access url")
	}
	return access, nil
}

func (c *HTTPClient) Accounts(ctx context.Context, accessURL string, start, end time.Time) (AccountSet, error) {
	var set AccountSet
	u, err := url.Parse(accessURL)
	if err != nil {
		return set, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/accounts"
	q := u.Query()
	q.Set("start-date", strconv.FormatInt(start.Unix(), 10))
	q.Set("end-date", strconv.FormatInt(end.Unix(), 10))
	q.Set("pending", "0")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return set, err
	}
	// Basic auth credentials embedded in the access URL are applied by net/http from req.URL.User.
	resp, err := c.http.Do(req)
	if err != nil {
		return set, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return set, fmt.Errorf("simplefin: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return set, fmt.Errorf("simplefin: decode: %w", err)
	}
	return set, nil
}
