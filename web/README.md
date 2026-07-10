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

## Voice Media

Sigmo uses browser WebRTC for call audio. The backend transcodes carrier
AMR/AMR-WB RTP to PCMU for the browser and encodes browser PCMU audio back to
the negotiated carrier AMR format.

Before creating a browser offer, the UI loads ICE configuration from
`GET /api/v1/call-media/ice-servers`. The backend uses the same ICE
configuration when creating the WebRTC answer. Offer, answer, and trickled ICE
candidates are exchanged over
`GET /api/v1/modems/{id}/calls/{callID}/webrtc/sessions` as a WebSocket.

Sigmo fetches short-lived Cloudflare TURN credentials from
`https://speed.cloudflare.com/turn-creds`. The backend filters WebRTC host
candidates to the system's default route interface so Docker bridge and other
non-routable interfaces are not advertised.

The backend WebRTC ICE UDP ports are pinned to `40000-40100`; expose that UDP
range on the server firewall.

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
