import resvgWasm from '@resvg/resvg-wasm/index_bg.wasm';
import { initWasm, Resvg } from '@resvg/resvg-wasm';

let wasmReady: Promise<void> | null = null;

function initializeWasm(): Promise<void> {
  if (!wasmReady) {
    wasmReady = initWasm(resvgWasm).catch((caught: unknown) => {
      wasmReady = null;
      throw caught;
    });
  }
  return wasmReady;
}

/** rasterizeSvg converts a text-free SVG into a 128-pixel-wide transparent PNG. */
export async function rasterizeSvg(svg: Uint8Array): Promise<Uint8Array> {
  await initializeWasm();
  const renderer = new Resvg(svg, {
    background: 'rgba(0,0,0,0)',
    fitTo: { mode: 'width', value: 128 },
  });
  const image = renderer.render();
  try {
    return image.asPng();
  } finally {
    image.free();
    renderer.free();
  }
}
