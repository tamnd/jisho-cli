package jisho

import (
	"context"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes jisho as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/jisho-cli/jisho"
//
// The init below registers it; the host then routes jisho:// URIs to the
// operations Register installs. The same Domain also builds the standalone
// jisho binary (see cli.NewApp), so the binary and a host share one source
// of truth.
func init() { kit.Register(Domain{}) }

// Domain is the jisho driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "jisho",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "jisho",
			Short:  "A command line for the Jisho Japanese dictionary.",
			Long: `A command line for the Jisho Japanese dictionary (jisho.org).

jisho reads public Jisho data over plain HTTPS, shapes it into clean records,
and prints output that pipes into the rest of your tools. No API key required,
nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/jisho-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search Japanese words by keyword",
		Args:    []kit.Arg{{Name: "query", Help: "English or Japanese search term"}}}, searchWords)

	kit.Handle(app, kit.OpMeta{Name: "kanji", Group: "read", List: true,
		Summary: "Look up a kanji character",
		Args:    []kit.Arg{{Name: "character", Help: "kanji character to look up"}}}, lookupKanji)

	kit.Handle(app, kit.OpMeta{Name: "random", Group: "read", List: true,
		Summary: "Get a random Japanese vocabulary word"}, randomWord)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- input structs ---

type searchInput struct {
	Query  string  `kit:"arg" help:"English or Japanese search term"`
	Common bool    `kit:"flag" help:"filter to common words only"`
	JLPT   string  `kit:"flag" help:"JLPT level filter (n5, n4, n3, n2, n1)"`
	Page   int     `kit:"flag" help:"page number"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type kanjiInput struct {
	Character string  `kit:"arg" help:"kanji character to look up"`
	Client    *Client `kit:"inject"`
}

type randomInput struct {
	Client *Client `kit:"inject"`
}

// --- handlers ---

func searchWords(ctx context.Context, in searchInput, emit func(*Word) error) error {
	q := buildQuery(in.Query, in.Common, in.JLPT)
	words, err := in.Client.SearchWords(ctx, q, in.Page)
	if err != nil {
		return mapErr(err)
	}
	for _, w := range words {
		if in.Limit > 0 && w == nil {
			break
		}
		if err := emit(w); err != nil {
			return err
		}
	}
	return nil
}

func lookupKanji(ctx context.Context, in kanjiInput, emit func(*Word) error) error {
	words, err := in.Client.LookupKanji(ctx, in.Character)
	if err != nil {
		return mapErr(err)
	}
	for _, w := range words {
		if err := emit(w); err != nil {
			return err
		}
	}
	return nil
}

func randomWord(ctx context.Context, in randomInput, emit func(*Word) error) error {
	words, err := in.Client.RandomWord(ctx)
	if err != nil {
		return mapErr(err)
	}
	for _, w := range words {
		if err := emit(w); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: URI-native string functions, pure and network-free ---

// Classify turns a slug into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	if input == "" {
		return "", "", errs.Usage("empty jisho reference")
	}
	return "word", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "word" {
		return "", errs.Usage("jisho has no resource type %q", uriType)
	}
	return "https://" + Host + "/search/" + id, nil
}

// mapErr converts a library error into the kit error kind that carries the
// right exit code.
func mapErr(err error) error {
	return err
}
