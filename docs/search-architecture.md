# Federated search architecture

`goskill find` separates provider transport code from the policy that decides
what users see:

```text
provider normalization
        ↓
concurrent aggregation
        ↓
source-based deduplication
        ↓
filtering and ranking
        ↓
human or JSON rendering
```

Each provider converts its response into `search.SearchResult`. The normalized
model retains a display name and description, canonical and installable
sources, author/repository/path data, adoption and trust signals, freshness,
provider metadata, and provenance. Provider-specific JSON fields and HTTP
assumptions stay in the provider package; ranking and rendering do not parse
provider responses.

`search.Aggregator` runs providers concurrently with the caller context. It
collects successful responses even if another provider fails, then calls
`Deduplicate`. Deduplication only merges records with a stable source identity
(normally repository, ref, and `SKILL.md` path); it never merges same-name
skills from unrelated sources. Merged results retain every contributing
provider and its provider-level signals.

After aggregation, `FilterAndRank` applies `--verified`, `--provider`, and the
requested deterministic sort. Default relevance prioritizes query matches in
the name, tags, category, description, and source. Trust/audit status,
popularity, freshness, and source completeness break ties. `popular` and
`newest` select the respective primary signal while retaining deterministic
secondary ordering.

## Adding a search provider

Implement the small `SearchProvider` interface:

```go
type SearchProvider interface {
    Name() string
    Search(context.Context, SearchQuery) ([]SearchResult, error)
}
```

Provider implementations should:

- normalize whitespace using the supplied `SearchQuery` and honor its limit;
- create bounded HTTP requests with the provided context and a provider timeout;
- normalize all available source, popularity, trust, and freshness fields into
  `SearchResult`;
- skip malformed individual records when a usable response remains possible;
- return a provider-scoped, credential-safe error for request or response
  failures; and
- avoid ranking, rendering, or cross-provider deduplication.

Add focused `httptest` coverage for success, empty results, malformed records,
timeouts/rate limits, authentication behavior, and aggregator partial failure.
For a configured optional provider, add a `ProviderRegistration` with a
`ProviderDescriptor` and factory. The descriptor declares whether auth is
required, public/private visibility, live/cached behavior, and deep-search
cost. Factories receive credentials through `ProviderCredentialResolver`; raw
credentials must not be accepted from configuration.

## Caches and failure handling

The TrueFoundry catalog store atomically writes cache snapshots and searches
them locally. A stale valid cache remains usable after a refresh failure. The
GitHub deep provider caches identical queries for the process lifetime.
Provider failures are status data, not global failures, unless no configured
provider completes successfully. Human output reports failures only when they
can make an empty search incomplete; JSON always includes provider outcomes.
