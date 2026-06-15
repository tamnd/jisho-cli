// Package jisho is the library behind the jisho command line:
// the HTTP client, request shaping, and the typed data models for the
// Jisho Japanese dictionary (jisho.org).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
package jisho

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Host is the site this client talks to.
const Host = "jisho.org"

// BaseURL is the API root every request is built from.
const BaseURL = "https://jisho.org/api/v1"

// DefaultUserAgent identifies the client to jisho.
const DefaultUserAgent = "jisho-cli/0.1 (tamnd87@gmail.com)"

// Config holds all tunable parameters for the client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		Rate:      1100 * time.Millisecond,
		Timeout:   15 * time.Second,
		Retries:   3,
		UserAgent: DefaultUserAgent,
	}
}

// Client talks to jisho.org over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- Wire types (raw API response) ---

type apiResponse struct {
	Meta struct {
		Status int `json:"status"`
	} `json:"meta"`
	Data []apiEntry `json:"data"`
}

type apiEntry struct {
	Slug     string   `json:"slug"`
	IsCommon bool     `json:"is_common"`
	Tags     []string `json:"tags"`
	JLPT     []string `json:"jlpt"`
	Japanese []struct {
		Word    string `json:"word"`
		Reading string `json:"reading"`
	} `json:"japanese"`
	Senses []struct {
		EnglishDefinitions []string `json:"english_definitions"`
		PartsOfSpeech      []string `json:"parts_of_speech"`
	} `json:"senses"`
}

// --- Output types ---

// Word is the canonical output record for a single dictionary entry.
type Word struct {
	Slug          string   `json:"slug" kit:"id"`
	Word          string   `json:"word"`
	Reading       string   `json:"reading"`
	IsCommon      bool     `json:"is_common"`
	JLPT          []string `json:"jlpt"`
	Tags          []string `json:"tags"`
	Definitions   []string `json:"definitions"`
	PartsOfSpeech []string `json:"parts_of_speech"`
}

// toWord converts a raw API entry into a Word.
func toWord(e apiEntry) *Word {
	w := &Word{
		Slug:     e.Slug,
		IsCommon: e.IsCommon,
		JLPT:     e.JLPT,
		Tags:     e.Tags,
	}
	if len(e.Japanese) > 0 {
		w.Word = e.Japanese[0].Word
		w.Reading = e.Japanese[0].Reading
	}
	for _, s := range e.Senses {
		w.Definitions = append(w.Definitions, s.EnglishDefinitions...)
		w.PartsOfSpeech = append(w.PartsOfSpeech, s.PartsOfSpeech...)
	}
	return w
}

// --- Client methods ---

// buildQuery assembles the Jisho keyword string from optional modifiers.
func buildQuery(q string, common bool, jlpt string) string {
	var parts []string
	if common {
		parts = append(parts, "#common")
	}
	if jlpt != "" {
		parts = append(parts, "#jlpt-"+jlpt)
	}
	parts = append(parts, q)
	return strings.Join(parts, " ")
}

// SearchWords searches for words matching the given query.
func (c *Client) SearchWords(ctx context.Context, query string, page int) ([]*Word, error) {
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	u := base + "/search/words?keyword=" + url.QueryEscape(query)
	if page > 1 {
		u += fmt.Sprintf("&page=%d", page)
	}
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]*Word, 0, len(resp.Data))
	for _, e := range resp.Data {
		out = append(out, toWord(e))
	}
	return out, nil
}

// LookupKanji looks up a kanji character using the #kanji tag.
func (c *Client) LookupKanji(ctx context.Context, char string) ([]*Word, error) {
	query := "#kanji " + char
	return c.SearchWords(ctx, query, 0)
}

// randomWords is a small built-in vocabulary list for the random command.
var randomWords = []string{
	"love", "water", "mountain", "flower", "river", "sky", "dream",
	"heart", "spring", "autumn", "wind", "fire", "earth", "music",
	"light", "night", "morning", "rain", "snow", "bird", "tree",
	"book", "time", "friend", "smile", "hope", "peace", "star",
	"moon", "sun", "way", "life", "world", "voice", "color",
}

// RandomWord fetches a random vocabulary word.
func (c *Client) RandomWord(ctx context.Context) ([]*Word, error) {
	word := randomWords[rand.Intn(len(randomWords))]
	query := "#common " + word
	return c.SearchWords(ctx, query, 0)
}
