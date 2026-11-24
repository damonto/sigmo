import SwiftUI

/// A profile's embedded brand takes priority over the shared carrier catalog.
struct CarrierLogo: View {
    var icon: String? = nil
    var plmn: String? = nil
    let regionCode: String
    var name = ""
    var size: CGFloat = 32

    var body: some View {
        render(samplesTint: false) { tile, _ in tile }
    }

    func withTint<Content: View>(@ViewBuilder _ content: @escaping (Tile, Color?) -> Content) -> some View {
        render(samplesTint: true, content)
    }

    private func render<Content: View>(samplesTint: Bool, @ViewBuilder _ content: @escaping (Tile, Color?) -> Content) -> some View {
        let url = AppLaunch.usesFixtures ? nil : CarrierLogoArtwork.catalogURL(plmn: plmn)
        return ArtworkView(source: .init(icon: icon, catalogURL: url), samplesTint: samplesTint) { artwork in
            let tile = Tile(image: artwork?.image, regionCode: regionCode, name: name, size: size)
            content(tile, artwork?.tint.map(Color.init(cgColor:)))
        }
    }

    struct Tile: View {
        let image: CGImage?
        let regionCode: String
        let name: String
        let size: CGFloat

        var body: some View {
            Group {
                if let image {
                    Image(decorative: image, scale: 1).resizable().scaledToFit()
                } else {
                    RegionFlag(regionCode: regionCode, size: size, background: .clear)
                }
            }
            .frame(width: size, height: size)
            .background(Color(uiColor: .tertiarySystemGroupedBackground))
            .clipShape(.rect(cornerRadius: size / 4))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(name)
            .accessibilityHidden(name.isEmpty)
        }
    }

    private struct ArtworkView<Content: View>: View {
        let source: CarrierLogoArtwork.Source
        let samplesTint: Bool
        @ViewBuilder let content: (CarrierLogoArtwork?) -> Content
        @State private var resolved: (source: CarrierLogoArtwork.Source, artwork: CarrierLogoArtwork)?

        var body: some View {
            // A SIM change invalidates both the badge and the tint immediately.
            content(resolved?.source == source ? resolved?.artwork : nil)
                .task(id: source) {
                    guard resolved?.source != source, !Task.isCancelled else { return }
                    resolved = nil
                    guard let artwork = await source.load(samplesTint: samplesTint), !Task.isCancelled else { return }
                    resolved = (source, artwork)
                }
        }
    }
}
