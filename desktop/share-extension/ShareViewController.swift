// Share extension of Unison.
//
// The extension appears in the Share menu of every app for files. It never
// talks to the daemon: it opens the shared files with the app of the bundle,
// and the window of the app asks for the devices of the offer.

import AppKit
import UniformTypeIdentifiers

/// Most files of one share, the limit of an offer of the daemon.
private let maxFiles = 32

/// How long the extension waits for the items before it hands over the ones
/// that arrived, so an item that never reports cannot leave it hanging.
private let loadTimeout: DispatchTimeInterval = .seconds(10)

@objc(ShareViewController)
final class ShareViewController: NSViewController {
    override var nibName: NSNib.Name? { nil }

    override func loadView() {
        view = NSView(frame: NSRect(x: 0, y: 0, width: 1, height: 1))
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        let providers = attachments()
        if providers.isEmpty {
            fail("the shared items contain no files")
            return
        }
        loadFileURLs(providers) { urls in
            self.open(urls)
        }
    }

    /// attachments returns the providers of the shared items that carry a file.
    private func attachments() -> [NSItemProvider] {
        let items = extensionContext?.inputItems.compactMap { $0 as? NSExtensionItem } ?? []
        let providers = items.flatMap { $0.attachments ?? [] }
        return providers
            .filter { $0.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) }
            .prefix(maxFiles)
            .map { $0 }
    }

    /// loadFileURLs reads the file URL of every provider and calls done on the
    /// main queue with the URLs that arrived. A provider that never reports
    /// cannot hold the extension, so the wait ends after loadTimeout.
    private func loadFileURLs(_ providers: [NSItemProvider], done: @escaping ([URL]) -> Void) {
        var urls = [URL?](repeating: nil, count: providers.count)
        var finished = false
        let finish = {
            guard !finished else { return }
            finished = true
            done(urls.compactMap { $0 })
        }
        let group = DispatchGroup()
        for (index, provider) in providers.enumerated() {
            group.enter()
            provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, _ in
                let url = Self.fileURL(item)
                DispatchQueue.main.async {
                    urls[index] = url
                    group.leave()
                }
            }
        }
        group.notify(queue: .main, execute: finish)
        DispatchQueue.main.asyncAfter(deadline: .now() + loadTimeout, execute: finish)
    }

    /// fileURL turns the item of a provider into a file URL.
    private static func fileURL(_ item: NSSecureCoding?) -> URL? {
        if let url = item as? URL, url.isFileURL {
            return url
        }
        if let data = item as? Data, let url = URL(dataRepresentation: data, relativeTo: nil),
            url.isFileURL
        {
            return url
        }
        return nil
    }

    /// open hands the files to the app of the bundle and ends the request.
    private func open(_ urls: [URL]) {
        if urls.isEmpty {
            fail("the shared items contain no files")
            return
        }
        guard let app = containingApp() else {
            fail("the extension is not inside the Unison app")
            return
        }
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = true
        NSWorkspace.shared.open(urls, withApplicationAt: app, configuration: configuration) {
            _, error in
            DispatchQueue.main.async {
                if let error {
                    self.fail("the Unison app did not open the files: \(error.localizedDescription)")
                    return
                }
                self.extensionContext?.completeRequest(returningItems: nil)
            }
        }
    }

    /// containingApp returns the app bundle the extension lives in:
    /// Unison.app/Contents/PlugIns/UnisonShare.appex.
    private func containingApp() -> URL? {
        let app = Bundle.main.bundleURL
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        if app.pathExtension != "app" {
            return nil
        }
        return app
    }

    /// fail ends the request with the given reason.
    private func fail(_ reason: String) {
        NSLog("Unison share: %@", reason)
        let error = NSError(
            domain: "app.unison.share", code: 1,
            userInfo: [NSLocalizedDescriptionKey: reason])
        extensionContext?.cancelRequest(withError: error)
    }
}
