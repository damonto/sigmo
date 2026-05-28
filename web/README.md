# Sigmo

This template should help get you started developing with Vue 3 in Vite.

## Recommended IDE Setup

[VS Code](https://code.visualstudio.com/) + [Vue (Official)](https://marketplace.visualstudio.com/items?itemName=Vue.volar) (and disable Vetur).

## Recommended Browser Setup

- Chromium-based browsers (Chrome, Edge, Brave, etc.):
  - [Vue.js devtools](https://chromewebstore.google.com/detail/vuejs-devtools/nhdogjmejiglipccpnnnanhbledajbpd)
  - [Turn on Custom Object Formatter in Chrome DevTools](http://bit.ly/object-formatters)
- Firefox:
  - [Vue.js devtools](https://addons.mozilla.org/en-US/firefox/addon/vue-js-devtools/)
  - [Turn on Custom Object Formatter in Firefox DevTools](https://fxdx.dev/firefox-devtools-custom-object-formatters/)

## Type Support for `.vue` Imports in TS

TypeScript cannot handle type information for `.vue` imports by default, so we replace the `tsc` CLI with `vue-tsc` for type checking. In editors, we need [Volar](https://marketplace.visualstudio.com/items?itemName=Vue.volar) to make the TypeScript language service aware of `.vue` types.

## Customize configuration

See [Vite Configuration Reference](https://vite.dev/config/).

## Project Setup

```sh
bun install
```

## Voice Media Codec

Sigmo forwards IMS RTP media to the browser. Browsers do not provide native
AMR/AMR-WB voice codecs, so Sigmo includes a browser codec adapter in
`src/lib/builtInAmrCodec.ts`.

The built-in codec supports full-duplex AMR-NB and AMR-WB calls through a
single OpenCORE AMR WebAssembly module:

- AMR-NB decode/encode: OpenCORE AMR
- AMR-WB decode: OpenCORE AMR
- AMR-WB encode: VisualOn `vo-amrwbenc`

Build the WASM module after installing Emscripten:

```sh
scripts/build-opencore-amr-wasm.sh
```

EVS remains intentionally unsupported in the browser adapter.

`media` is the negotiated call media object from the backend:

```ts
type CallMediaInfo = {
  codec: string
  payloadType: number
  clockRate: number
  channels: number
  dtmfPayloadType: number
  dtmfClockRate: number
  ptimeMillis: number
}
```

The codec adapter works with one AMR speech frame at a time:

```ts
type AmrFrame = {
  frameType: number
  quality: boolean
  data: Uint8Array
}

type PcmFrame = {
  samples: Float32Array
  sampleRate: number
}
```

The browser pipeline already handles RTP packetization, AMR payload
parsing/building, microphone capture, playback scheduling, and mono resampling.
The codec adapter only converts AMR speech frames to PCM and PCM to AMR speech
frames.

### Compile and Hot-Reload for Development

```sh
bun dev
```

### Type-Check, Compile and Minify for Production

```sh
bun run build
```

### Run Unit Tests with [Vitest](https://vitest.dev/)

```sh
bun test:unit
```

### Lint with [ESLint](https://eslint.org/)

```sh
bun lint
```
