# jisho

A command line for the Jisho Japanese dictionary.

`jisho` is a single pure-Go binary. It reads public Jisho data (jisho.org)
over plain HTTPS, shapes it into clean records, and prints output that pipes
into the rest of your tools. No API key, nothing to run alongside it.

The same package is also a [resource-URI driver](#use-it-as-a-resource-uri-driver),
so a host program like [ant](https://github.com/tamnd/ant) can address
jisho as `jisho://` URIs.

## Install

```bash
go install github.com/tamnd/jisho-cli/cmd/jisho@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/tamnd/jisho-cli/releases), or run
the container image:

```bash
docker run --rm ghcr.io/tamnd/jisho:latest --help
```

## Usage

```bash
jisho search hello                     # search by English or Japanese
jisho search 日本語 -o json            # as JSON, ready for jq
jisho search hello --common            # common words only
jisho search hello --jlpt n5           # JLPT N5 level only
jisho search hello --page 2            # second page of results
jisho kanji 日                         # look up a kanji character
jisho random                           # a random vocabulary word
jisho --help                           # the whole command tree
```

Every command shares one output contract: `-o table|json|jsonl|csv|tsv|url|raw`,
`--fields` to pick columns, `--template` for a custom line, and `-n` to limit.
The default adapts to where output goes (a table on a terminal, JSONL in a
pipe), so the same command reads well by hand and parses cleanly downstream.

## Serve it

The same operations are available over HTTP and as an MCP tool set for agents,
with no extra code:

```bash
jisho serve --addr :7777    # GET /v1/page/<path>  returns NDJSON
jisho mcp                   # speak MCP over stdio
```

## Use it as a resource-URI driver

`jisho` registers a `jisho` domain the way a program registers a
database driver with `database/sql`. A host enables it with one blank import:

```go
import _ "github.com/tamnd/jisho-cli/jisho"
```

Then [ant](https://github.com/tamnd/ant) (or any program that links the package)
dereferences `jisho://` URIs without knowing anything about jisho:

```bash
ant get jisho://page/<path>   # fetch the record
ant cat jisho://page/<path>   # just the body text
ant ls  jisho://page/<path>   # the pages it links to, each addressable
ant url jisho://page/<path>   # the live https URL
```

## Development

```
cmd/jisho/   thin main: hands cli.NewApp to kit.Run
cli/                 assembles the kit App from the jisho domain
jisho/                the library: HTTP client, data models, and domain.go (the driver)
docs/                tago documentation site
```

```bash
make build      # ./bin/jisho
make test       # go test ./...
make vet        # go vet ./...
```

## Releasing

Push a version tag and GitHub Actions runs GoReleaser, which builds the
archives, Linux packages, the multi-arch GHCR image, checksums, SBOMs, and a
cosign signature:

```bash
git tag v0.1.0
git push --tags
```

The Homebrew and Scoop steps self-disable until their tokens exist, so the first
release works with no extra secrets.

## License

Apache-2.0. See [LICENSE](LICENSE).
