package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

const (
	pageSize = 100
	// maxPages bounds a single listing to protect the shared hourly quota.
	maxPages = 20
)

// GetAll fetches every page of a list endpoint using page[number] and
// page[size].
func GetAll[T any](ctx context.Context, c *Client, path string, query url.Values, ttl time.Duration) ([]T, error) {
	var all []T
	for page := 1; page <= maxPages; page++ {
		resp, err := c.get(ctx, path, pageQuery(query, page), ttl)
		if err != nil {
			return nil, err
		}
		var items []T
		if err := json.Unmarshal(resp.Body, &items); err != nil {
			return nil, fmt.Errorf("decoding page %d of %s: %w", page, path, err)
		}
		all = append(all, items...)
		if len(items) < pageSize || lastPage(resp, page) {
			return all, nil
		}
	}
	return all, nil
}

// pageQuery copies query and sets the pagination parameters.
func pageQuery(query url.Values, page int) url.Values {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("page[size]", strconv.Itoa(pageSize))
	q.Set("page[number]", strconv.Itoa(page))
	return q
}

// lastPage uses X-Total and X-Per-Page when the server sends them.
func lastPage(resp *Response, page int) bool {
	if resp.Header == nil {
		return false
	}
	total, err1 := strconv.Atoi(resp.Header.Get("X-Total"))
	per, err2 := strconv.Atoi(resp.Header.Get("X-Per-Page"))
	return err1 == nil && err2 == nil && per > 0 && page*per >= total
}
