import Perception
import SwiftUI

struct SwipletOpener: ViewModifier {
    @State private var preview: SwipletPreview?
    @State private var alert: AlertState?

    func body(content: Content) -> some View {
        content
            .onOpenURL { url in
                guard #available(iOS 17.4, *), url.isFileURL else { return }
                Task {
                    do {
                        let opened = try await SwipletStore.shared.open(url)
                        if let preview {
                            SwipletStore.shared.discard(preview)
                        }
                        preview = opened
                    } catch {
                        alert = AlertState(String(localized: "Can’t Open the Swiplet"), error: error)
                    }
                }
            }
            .sheet(item: $preview) { preview in
                if #available(iOS 17.4, *) {
                    NavigationStackCompat {
                        OpenSwipletView(preview: preview)
                    }
                }
            }
            .alert($alert)
    }
}

@available(iOS 17.4, *)
struct OpenSwipletView: View {
    let preview: SwipletPreview
    var onImported: () -> Void = {}

    @Environment(\.dismiss) private var dismiss
    @State private var isImporting = false
    @State private var alert: AlertState?

    private let swiplets = SwipletStore.shared

    var body: some View {
        WithPerceptionTracking {
            let installed = swiplets.versions[preview.id]
            Form {
                Section {
                    HStack(spacing: 12) {
                        SwipletIconImage(icon: preview.icon, size: 40)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: preview.title)
                                .lineLimit(1)
                            if !preview.version.isEmpty {
                                Text(verbatim: preview.version)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                } footer: {
                    if let installed {
                        if installed.isEmpty {
                            Text("The installed copy will be replaced.")
                        } else {
                            Text("Version \(installed) is installed and will be replaced.")
                        }
                    }
                }
            }
            .disabled(isImporting)
            .interactiveDismissDisabled(isImporting)
            .navigationTitle(installed == nil ? Text("Add Swiplet") : Text("Update Swiplet"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        swiplets.discard(preview)
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isImporting {
                        ProgressView()
                    } else {
                        Button(action: importSwiplet) {
                            if installed == nil {
                                Text("Add")
                            } else {
                                Text("Update")
                            }
                        }
                    }
                }
            }
            .alert($alert)
        }
    }

    private func importSwiplet() {
        isImporting = true
        Task {
            do {
                try await swiplets.importSwiplet(preview)
                onImported()
                dismiss()
            } catch {
                alert = AlertState(String(localized: "Can’t Import the Swiplet"), error: error)
                isImporting = false
            }
        }
    }
}
