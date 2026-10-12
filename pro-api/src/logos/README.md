# Carrier logos

`carriers.json` contains only `{ "logo": "filename.png", "plmns": ["310070"] }`
records. PLMNs stay strings to preserve leading zeroes. Images are stored at
`logos/{filename}` in the shared private `sigmo` R2 bucket. Release artifacts
keep their existing `stable/` and `dev/` paths in the same bucket.

The public `GET/HEAD /logo/{plmn}` endpoint looks up a filename in this catalog.
It returns PNG, ETag, and one-day caching. Missing images return a five-minute
cached 404; storage failures return an uncached 503. The route never reads
release objects or calls an external logo provider. Web/iOS try an embedded
profile icon first, then this endpoint, then their built-in flag.

The one-time import merged the PROXIES.SX registry and the supplied Netify
`logos.json`, keeping existing mappings where sources overlapped. The resulting
186 PNGs cover 365 PLMNs. PROXIES.SX is attributed under CC BY 4.0; the supplied
Netify file does not specify a license. Logo.dev self-hosting authorization was
confirmed separately. Full provenance and import reports are retained with the
local images in `build/carrier-logos/`.

Logo.dev images use `size=128&format=png&retina=true` (256×256 pixels). Netify
images are 128×128 PNGs; non-square images were fitted proportionally with
transparent padding, and ICOs were converted. Client display size is independent
of the pixel dimensions and matches the built-in flag area.

For a later data update, upload PNGs to `sigmo/logos/`, edit this compact catalog,
and deploy the Worker. Clients continue to use the same `/logo/{plmn}` URL.
