# Accepted govulncheck findings

These OSV IDs are explicitly accepted residuals for release binaries. Any other
finding must fail `just compliance-check`.

| OSV ID | Module | Reason | Review by |
|---|---|---|---|
| `GO-2026-4740` | `github.com/shamaton/msgpack/v3` | Reachable through embedded Restish; no fixed upstream version | 2026-10-27 |
| `GO-2026-4513` | `github.com/shamaton/msgpack/v3` | Reachable through embedded Restish; no fixed upstream version | 2026-10-27 |

Accepted IDs (one per line):

```text
GO-2026-4740
GO-2026-4513
```
