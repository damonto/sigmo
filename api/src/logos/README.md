# Carrier logos

The Worker synchronizes
[`https://simdex.simb.workers.dev/data/index.json`](https://simdex.simb.workers.dev/data/index.json)
daily at `03:17 UTC`. It validates the upstream index, keeps the first record
for each 5- or 6-digit PLMN, and atomically writes the normalized map to
`carrier-logos/catalog.json` in the private `sigmo` R2 bucket. A failed refresh
leaves the previous catalog unchanged.

`GET|HEAD /logo/{plmn}` reads that catalog and proxies the selected
`/icons/...` path from Simdex. SVG files containing `<text>`, `<tspan>`, or
`<textPath>` elements are discarded and return the same short-lived 404 as an
unknown PLMN, allowing Web and iOS to show their built-in flags. Other SVGs are
rasterized to 128-pixel-wide transparent PNGs with
`@resvg/resvg-wasm`. PNG sources are validated and passed through without
re-encoding.

Every successful response has `Content-Type: image/png`, an ETag, and one-day
browser/edge caching. The Cloudflare Cache API keys rendered PNGs by source icon
path, so PLMNs sharing an icon reuse one conversion. Storage, upstream, and
rasterization failures return an uncached 503.

The catalog is seeded lazily on the first valid logo request after a fresh
deployment. `bun run deploy` includes the daily Cron Trigger from
`wrangler.jsonc`; no manual image upload is required.
