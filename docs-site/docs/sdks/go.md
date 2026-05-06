# Go client

Lives in-tree at `github.com/felixgeelhaar/mnemos/client`. Same package as the Mnemos server itself, so it tracks the wire format precisely.

```go
import "github.com/felixgeelhaar/mnemos/client"

c := client.New("http://localhost:7777", client.WithToken(os.Getenv("MNEMOS_JWT")))

events, err := c.Events().List(ctx)
hits, err := c.Search(ctx, "dietary restrictions", client.SearchOptions{TopK: 5, MinTrust: 0.5})
block, err := c.Context(ctx, "chat-session-1", client.ContextOptions{})
```

## Surface

| Method | Wraps |
|---|---|
| `client.New(baseURL, ...Option)` | constructor |
| `WithToken / WithHTTPClient / WithTimeout / WithRetry / WithLogger` | options |
| `c.Health(ctx)` | `GET /health` |
| `c.Metrics(ctx)` | `GET /v1/metrics` |
| `c.Events()` builder | `/v1/events` |
| `c.Claims()` builder | `/v1/claims` |
| `c.Relationships()` builder | `/v1/relationships` |
| `c.Embeddings()` builder | `/v1/embeddings` |
| `c.Search(ctx, query, opts)` | `GET /v1/search` |
| `c.Context(ctx, runID, opts)` | `GET /v1/context` |

Source: [github.com/felixgeelhaar/mnemos/tree/main/client](https://github.com/felixgeelhaar/mnemos/tree/main/client).
