import ImageIO
import UIKit

struct CarrierLogoArtwork: Sendable {
    let image: CGImage
    let tint: CGColor?

    struct Source: Equatable, Sendable {
        let icon: String?
        let catalogURL: URL?

        @concurrent func load(samplesTint: Bool, maximumSize: Int = 384) async -> CarrierLogoArtwork? {
            guard !Task.isCancelled else { return nil }
            if let image = embeddedImage(maximumSize: maximumSize) {
                return CarrierLogoArtwork(image: image, samplesTint: samplesTint)
            }
            guard let catalogURL, let (data, response) = try? await URLSession.shared.data(from: catalogURL),
                  (response as? HTTPURLResponse)?.statusCode == 200, !Task.isCancelled,
                  let image = CarrierLogoArtwork.thumbnail(from: data, maximumSize: maximumSize) else { return nil }
            return CarrierLogoArtwork(image: image, samplesTint: samplesTint)
        }

        private func embeddedImage(maximumSize: Int) -> CGImage? {
            guard let icon, icon.hasPrefix("data:image/"), let comma = icon.firstIndex(of: ","),
                  icon[..<comma].hasSuffix(";base64"),
                  let data = Data(base64Encoded: String(icon[icon.index(after: comma)...])) else { return nil }
            return CarrierLogoArtwork.thumbnail(from: data, maximumSize: maximumSize)
        }
    }

    private init(image: CGImage, samplesTint: Bool) {
        self.image = image
        tint = samplesTint ? Self.dominantColor(in: image) : nil
    }

    static func catalogURL(plmn: String?) -> URL? {
        guard let plmn, (5...6).contains(plmn.utf8.count),
              plmn.utf8.allSatisfy({ (48...57).contains($0) }) else { return nil }
        return URL(string: "https://sigmo.952007.xyz/logo/\(plmn)")
    }

    private static func thumbnail(from data: Data, maximumSize: Int) -> CGImage? {
        guard let source = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary) else {
            return nil
        }
        // Keep enough pixels for the largest Dynamic Type avatar at 3x.
        return CGImageSourceCreateThumbnailAtIndex(source, 0, [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maximumSize,
            kCGImageSourceShouldCacheImmediately: true
        ] as CFDictionary)
    }

    static func dominantColor(in image: CGImage) -> CGColor? {
        guard let space = CGColorSpace(name: CGColorSpace.sRGB) else { return nil }
        let size = 32
        let format = CGBitmapInfo.byteOrder32Big.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue
        var pixels = [UInt8](repeating: 0, count: size * size * 4)
        let drawn = pixels.withUnsafeMutableBytes { buffer -> Bool in
            guard let context = CGContext(data: buffer.baseAddress, width: size, height: size, bitsPerComponent: 8,
                                          bytesPerRow: size * 4, space: space, bitmapInfo: format) else { return false }
            context.interpolationQuality = .medium
            context.draw(image, in: CGRect(x: 0, y: 0, width: size, height: size))
            return true
        }
        guard drawn else { return nil }

        // Rank chromatic pixels by hue so the logo's backing does not determine the tint.
        var bins = Array(repeating: ColorBin(), count: 12)
        for offset in stride(from: 0, to: pixels.count, by: 4) {
            let alpha = CGFloat(pixels[offset + 3])
            guard alpha >= 128 else { continue }
            let red = CGFloat(pixels[offset]) / alpha
            let green = CGFloat(pixels[offset + 1]) / alpha
            let blue = CGFloat(pixels[offset + 2]) / alpha
            let color = UIColor(red: red, green: green, blue: blue, alpha: 1)
            var hue: CGFloat = 0, saturation: CGFloat = 0, brightness: CGFloat = 0
            guard color.getHue(&hue, saturation: &saturation, brightness: &brightness, alpha: nil),
                  saturation >= 0.2, brightness >= 0.22 else { continue }
            let index = Int((hue * CGFloat(bins.count)).rounded()) % bins.count
            let weight = saturation * alpha
            bins[index].weight += weight
            bins[index].red += red * weight
            bins[index].green += green * weight
            bins[index].blue += blue * weight
        }
        guard let best = bins.max(by: { $0.weight < $1.weight }), best.weight > 0 else { return nil }
        return CGColor(colorSpace: space, components: [best.red / best.weight, best.green / best.weight, best.blue / best.weight, 1])
    }

    private struct ColorBin {
        var weight: CGFloat = 0
        var red: CGFloat = 0
        var green: CGFloat = 0
        var blue: CGFloat = 0
    }
}
