package jisho

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := NewClient()
	c.Rate = 0 // no pacing in tests
	c.BaseURL = ts.URL
	return ts, c
}

func wordResponse(entries []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"meta": map[string]interface{}{"status": 200},
		"data": entries,
	}
}

func sampleEntry() map[string]interface{} {
	return map[string]interface{}{
		"slug":      "hello",
		"is_common": true,
		"jlpt":      []string{"jlpt-n5"},
		"tags":      []string{"wanikani5"},
		"japanese": []map[string]interface{}{
			{"word": "今日は", "reading": "こんにちは"},
		},
		"senses": []map[string]interface{}{
			{
				"english_definitions": []string{"Hello", "Good day"},
				"parts_of_speech":    []string{"Interjection"},
			},
		},
	}
}

func TestGet(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	})
	body, err := c.Get(context.Background(), c.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	})
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), c.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearchWords(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/words" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		kw := r.URL.Query().Get("keyword")
		if kw == "" {
			t.Error("missing keyword param")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wordResponse([]map[string]interface{}{sampleEntry()}))
	})

	words, err := c.SearchWords(context.Background(), "hello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 1 {
		t.Fatalf("got %d words, want 1", len(words))
	}
	w := words[0]
	if w.Slug != "hello" {
		t.Errorf("Slug = %q, want %q", w.Slug, "hello")
	}
	if !w.IsCommon {
		t.Error("IsCommon = false, want true")
	}
	if w.Word != "今日は" {
		t.Errorf("Word = %q, want 今日は", w.Word)
	}
	if w.Reading != "こんにちは" {
		t.Errorf("Reading = %q, want こんにちは", w.Reading)
	}
	if len(w.Definitions) == 0 || w.Definitions[0] != "Hello" {
		t.Errorf("Definitions = %v, want first=Hello", w.Definitions)
	}
	if len(w.JLPT) == 0 || w.JLPT[0] != "jlpt-n5" {
		t.Errorf("JLPT = %v, want [jlpt-n5]", w.JLPT)
	}
}

func TestSearchWordsPagination(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page != "2" {
			t.Errorf("page param = %q, want 2", page)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wordResponse([]map[string]interface{}{}))
	})

	_, err := c.SearchWords(context.Background(), "hello", 2)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLookupKanji(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		kw := r.URL.Query().Get("keyword")
		if kw != "#kanji 日" {
			t.Errorf("keyword = %q, want %q", kw, "#kanji 日")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wordResponse([]map[string]interface{}{sampleEntry()}))
	})

	words, err := c.LookupKanji(context.Background(), "日")
	if err != nil {
		t.Fatal(err)
	}
	if len(words) == 0 {
		t.Fatal("got 0 words, want at least 1")
	}
}

func TestBuildQuery(t *testing.T) {
	cases := []struct {
		q      string
		common bool
		jlpt   string
		want   string
	}{
		{"hello", false, "", "hello"},
		{"hello", true, "", "#common hello"},
		{"hello", false, "n5", "#jlpt-n5 hello"},
		{"hello", true, "n4", "#common #jlpt-n4 hello"},
	}
	for _, tc := range cases {
		got := buildQuery(tc.q, tc.common, tc.jlpt)
		if got != tc.want {
			t.Errorf("buildQuery(%q, %v, %q) = %q, want %q", tc.q, tc.common, tc.jlpt, got, tc.want)
		}
	}
}

func TestRandomWord(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		kw := r.URL.Query().Get("keyword")
		// must start with #common
		if len(kw) < 7 || kw[:7] != "#common" {
			t.Errorf("keyword = %q, want to start with #common", kw)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wordResponse([]map[string]interface{}{sampleEntry()}))
	})

	words, err := c.RandomWord(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(words) == 0 {
		t.Fatal("got 0 words, want at least 1")
	}
}

func TestToWord_EmptyJapanese(t *testing.T) {
	e := apiEntry{
		Slug:     "test",
		IsCommon: false,
		JLPT:     []string{},
		Tags:     []string{},
		Japanese: []struct {
			Word    string `json:"word"`
			Reading string `json:"reading"`
		}{},
		Senses: []struct {
			EnglishDefinitions []string `json:"english_definitions"`
			PartsOfSpeech      []string `json:"parts_of_speech"`
		}{
			{EnglishDefinitions: []string{"test"}, PartsOfSpeech: []string{"Noun"}},
		},
	}
	w := toWord(e)
	if w.Word != "" {
		t.Errorf("Word = %q, want empty", w.Word)
	}
	if w.Reading != "" {
		t.Errorf("Reading = %q, want empty", w.Reading)
	}
	if len(w.Definitions) != 1 || w.Definitions[0] != "test" {
		t.Errorf("Definitions = %v, want [test]", w.Definitions)
	}
}
