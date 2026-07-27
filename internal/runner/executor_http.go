package runner

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const DefaultRequestTimeout = 10 * time.Second

var (
	ErrUnexpectedStatus     = errors.New("unexpected HTTP status")
	ErrResponseBodyTooLarge = errors.New("response body too large")
)

func cloneHTTPClient(source *http.Client) *http.Client {
	clone := *source
	if clone.Timeout == 0 {
		clone.Timeout = DefaultRequestTimeout
	}
	// A generated short URL deliberately points outside the cluster. Kurama is
	// measuring the target API's redirect response, not following that URL.
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func (e *Executor) resolveRequestURL(path string) (*url.URL, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	resolved := e.baseURL.ResolveReference(reference)
	if resolved.Scheme != e.baseURL.Scheme || resolved.Host != e.baseURL.Host {
		return nil, fmt.Errorf("rendered path attempted to override target origin")
	}
	return resolved, nil
}

func readBoundedResponse(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, MaxResponseBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(body) > MaxResponseBodyBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", ErrResponseBodyTooLarge, MaxResponseBodyBytes)
	}
	return body, nil
}

func containsStatus(expected []int, actual int) bool {
	for _, status := range expected {
		if status == actual {
			return true
		}
	}
	return false
}
