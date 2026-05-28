import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

type OpenCoreAmrFactory = (options?: {
  locateFile?: (path: string, prefix: string) => string
  wasmBinary?: ArrayBuffer
}) => Promise<unknown>

declare global {
  var __sigmoOpenCoreAmrFactory: OpenCoreAmrFactory | undefined
}

export const installGeneratedOpenCoreAmrFactory = async () => {
  const modulePath = resolve(process.cwd(), 'public/codecs/opencore-amr.js')
  const wasmPath = resolve(process.cwd(), 'public/codecs/opencore-amr.wasm')
  const moduleURL = pathToFileURL(modulePath).href
  const [{ default: factory }, wasmBytes] = await Promise.all([
    import(/* @vite-ignore */ moduleURL) as Promise<{ default: OpenCoreAmrFactory }>,
    readFile(wasmPath),
  ])
  const wasmBinary = wasmBytes.buffer.slice(
    wasmBytes.byteOffset,
    wasmBytes.byteOffset + wasmBytes.byteLength,
  )

  globalThis.__sigmoOpenCoreAmrFactory = (options) => factory({ ...options, wasmBinary })
}

export const uninstallGeneratedOpenCoreAmrFactory = () => {
  delete globalThis.__sigmoOpenCoreAmrFactory
}
