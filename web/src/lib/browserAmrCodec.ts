import type { CallMediaInfo } from '@/types/call'

export const hasBrowserAmrCodec = () => typeof window !== 'undefined'

export const createBrowserAmrCodec = (media: CallMediaInfo) => {
  if (media.codec.trim().toUpperCase() === 'PCMU') {
    return {
      decode: () => {
        throw new Error('PCMU is handled by the media pipeline')
      },
      encode: () => {
        throw new Error('PCMU is handled by the media pipeline')
      },
    }
  }
  return loadBuiltInCodec(media)
}

const loadBuiltInCodec = async (media: CallMediaInfo) => {
  const builtIn = await import('./builtInAmrCodec')
  if (builtIn.builtInAmrCodecSupports(media)) {
    return builtIn.createBuiltInAmrCodec(media)
  }
  throw new Error(`${media.codec} codec is not available`)
}
