package proxy

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const streamChunkSize = 4096

type Proxy struct {
	Target      string
	client      *http.Client
	maxBodySize int64
}

var sensitiveRequestHeaders = map[string]bool{
	"host":              true,
	"x-api-key":         true,
	"authorization":     true,
	"cookie":            true,
	"set-cookie":        true,
	"x-forwarded-for":   true,
	"x-forwarded-proto": true,
	"x-real-ip":         true,
	"cf-connecting-ip":  true,
	"cf-ray":            true,
	"connection":        true,
	"upgrade":           true,
}

var sensitiveResponseHeaders = map[string]bool{
	"server":          true,
	"x-powered-by":    true,
	"x-aspnet-version": true,
}

func New(target string, maxBodySizeMB int) *Proxy {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
	return &Proxy{
		Target:      target,
		maxBodySize: int64(maxBodySizeMB) * 1024 * 1024,
		client: &http.Client{
			Timeout:   120 * time.Second,
			Transport: transport,
		},
	}
}

type Result struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

type StreamChunk struct {
	Data string
	Err  error
}

func validatePath(path string) error {
	if strings.ContainsAny(path, "\r\n\000") {
		return errors.New("path contains forbidden characters")
	}
	if strings.Contains(path, "@") || strings.Contains(path, "..") {
		return errors.New("invalid path")
	}
	return nil
}

func buildURL(base, path string) (string, error) {
	pathPart, queryPart, _ := strings.Cut(path, "?")
	if queryPart != "" && strings.ContainsAny(queryPart, "\r\n\000") {
		return "", errors.New("query contains forbidden characters")
	}
	safeURL, err := url.JoinPath(base, pathPart)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if queryPart != "" {
		safeURL += "?" + queryPart
	}
	return safeURL, nil
}

func filterRequestHeaders(headers map[string]string) http.Header {
	out := make(http.Header)
	for k, v := range headers {
		if sensitiveRequestHeaders[strings.ToLower(k)] {
			continue
		}
		out.Set(k, v)
	}
	return out
}

func filterResponseHeaders(resp *http.Response) map[string]string {
	out := make(map[string]string)
	for k, v := range resp.Header {
		if sensitiveResponseHeaders[strings.ToLower(k)] {
			continue
		}
		out[k] = v[0]
	}
	return out
}

func (p *Proxy) Forward(method, path string, headers map[string]string, body string) (*Result, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}

	safeURL, err := buildURL(p.Target, path)
	if err != nil {
		return nil, err
	}

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, safeURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header = filterRequestHeaders(headers)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forwarding request: %w", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	n, err := io.Copy(&buf, io.LimitReader(resp.Body, p.maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if n > p.maxBodySize {
		return nil, fmt.Errorf("response body exceeds %d bytes (%d MB)", p.maxBodySize, p.maxBodySize/(1024*1024))
	}

	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    filterResponseHeaders(resp),
		Body:       buf.String(),
	}, nil
}

func (p *Proxy) ForwardStream(method, path string, headers map[string]string, body string) (<-chan StreamChunk, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}

	safeURL, err := buildURL(p.Target, path)
	if err != nil {
		return nil, err
	}

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, safeURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header = filterRequestHeaders(headers)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forwarding request: %w", err)
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		buf := make([]byte, streamChunkSize)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				ch <- StreamChunk{Data: string(buf[:n])}
			}
			if err != nil {
				if err != io.EOF {
					ch <- StreamChunk{Err: err}
				}
				return
			}
		}
	}()

	return ch, nil
}
