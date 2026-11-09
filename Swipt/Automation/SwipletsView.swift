import Perception
import SwiftUI

@available(iOS 17.4, *)
struct SwipletsView: View {
    @Environment(\.scenePhase) private var scenePhase
    @State private var isAddingSwiplet = false
    @State private var isShowingUpdates = false
    @State private var swipletToDelete: String?
    @State private var swipletInfo: String?
    @State private var outdatedSwiplet: String?
    @State private var alert: AlertState?

    private let swiplets = SwipletStore.shared

    var body: some View {
        WithPerceptionTracking {
            Group {
                if swiplets.ids.isEmpty {
                    ContentUnavailableViewCompat {
                        Label("No Swiplets", systemImage: "wand.and.stars")
                    } description: {
                        Text("Tap \(Image(systemName: "plus")) to add a swiplet.")
                    }
                } else {
                    Form {
                        swipletsSection
                    }
                }
            }
            .navigationTitle("Swiplets")
            .navigationDestination(item: $swipletInfo) { id in
                SwipletInfoView(id: id)
            }
            .toolbar {
                if !swiplets.updates.isEmpty {
                    ToolbarItem(placement: .primaryAction) {
                        Button {
                            isShowingUpdates = true
                        } label: {
                            Label("Updates", systemImage: "arrow.down.circle")
                        }
                    }
                }
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        isAddingSwiplet = true
                    } label: {
                        Label("Add Swiplet", systemImage: "plus")
                    }
                }
            }
            .sheet(isPresented: $isAddingSwiplet) {
                NavigationStackCompat {
                    NewSwipletView()
                }
            }
            .sheet(isPresented: $isShowingUpdates) {
                NavigationStackCompat {
                    SwipletUpdatesView()
                }
            }
            .onAppear {
                SwipletHost.shared.prepare(swiplets.interfaces)
            }
            .onChange(of: swiplets.interfaces) {
                SwipletHost.shared.prepare(swiplets.interfaces)
            }
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                repeat {
                    await swiplets.refreshRepositories()
                } while (try? await Task.sleep(for: .seconds(SwipletStore.updateInterval))) != nil
            }
            .alert(
                "Delete “\(swipletToDelete.map { swiplets.titles[$0] ?? $0 } ?? "")”?",
                isPresented: Binding(get: { swipletToDelete != nil }, set: { if !$0 { swipletToDelete = nil } })
            ) {
                Button("Cancel", role: .cancel) {}
                Button("Delete", role: .destructive) {
                    if let swipletToDelete {
                        Task {
                            do {
                                try await swiplets.remove(swipletToDelete)
                            } catch {
                                alert = AlertState(String(localized: "Can’t Delete the Swiplet"), error: error)
                            }
                        }
                    }
                    swipletToDelete = nil
                }
            }
            .alert(
                "Swiplet Outdated",
                isPresented: Binding(get: { outdatedSwiplet != nil }, set: { if !$0 { outdatedSwiplet = nil } }),
                presenting: outdatedSwiplet
            ) { _ in
                Button("OK") {}
            } message: { name in
                Text("“\(swiplets.titles[name] ?? name)” needs to be updated to work with this version of Swipt.")
            }
            .alert($alert)
        }
    }

    private var swipletsSection: some View {
        Section {
            ForEach(swiplets.ids, id: \.self, content: swipletRow)
                .onMove { source, destination in
                    var ids = swiplets.ids
                    ids.move(fromOffsets: source, toOffset: destination)
                    swiplets.reorder(ids)
                }
        }
    }

    private func swipletRow(_ name: String) -> some View {
        Button {
            if swiplets.outdated.contains(name) {
                outdatedSwiplet = name
            } else if let entry = swiplets.interfaces[name] {
                SwipletPresentation.open(name: name, entry: entry)
            }
        } label: {
            swipletLabel(name)
        }
        .tint(.primary)
        .swipeActions(edge: .trailing) {
            Button {
                swipletToDelete = name
            } label: {
                Label("Delete", systemImage: "trash")
            }
            .tint(.red)
        }
        .contextMenu {
            Button {
                swipletInfo = name
            } label: {
                Label("Info", systemImage: "info.circle")
            }
            Button {
                share(name)
            } label: {
                Label("Share", systemImage: "square.and.arrow.up")
            }
            if swiplets.update(of: name) == nil, let repository = swiplets.repositoryURLs[name], swiplets.repository(repository) != nil {
                Button {
                    checkForUpdate(name, in: repository)
                } label: {
                    Label("Check for Updates…", systemImage: "arrow.clockwise")
                }
            }
            Button(role: .destructive) {
                swipletToDelete = name
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
    }

    private func swipletLabel(_ name: String) -> some View {
        HStack(spacing: 12) {
            SwipletIconImage(icon: swiplets.icons[name] ?? .default, size: 40)
            Text(verbatim: swiplets.titles[name] ?? name)
                .lineLimit(1)
        }
    }

    private func share(_ name: String) {
        Task {
            do {
                let file = try await swiplets.export(name)
                guard let top = topViewController() else { return }
                let controller = UIActivityViewController(activityItems: [file], applicationActivities: nil)
                controller.completionWithItemsHandler = { _, _, _, _ in
                    try? FileManager.default.removeItem(at: file.deletingLastPathComponent())
                }
                if let popover = controller.popoverPresentationController {
                    popover.sourceView = top.view
                    popover.sourceRect = CGRect(origin: CGPoint(x: top.view.bounds.midX, y: top.view.bounds.midY), size: .zero)
                    popover.permittedArrowDirections = []
                }
                top.present(controller, animated: true)
            } catch {
                alert = AlertState(String(localized: "Can’t Share the Swiplet"), error: error)
            }
        }
    }

    private func checkForUpdate(_ name: String, in repository: String) {
        Task {
            do {
                try await swiplets.refreshRepository(repository, force: true)
                if swiplets.update(of: name) == nil {
                    alert = AlertState(title: String(localized: "You’re Up to Date"))
                }
            } catch {
                alert = AlertState(String(localized: "Couldn’t Check for Updates"), error: error)
            }
        }
    }
}

@available(iOS 17.4, *)
struct SwipletUpdatesView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var alert: AlertState?

    private let swiplets = SwipletStore.shared

    var body: some View {
        WithPerceptionTracking {
            let pending = swiplets.updates
            Form {
                Section {
                    Button("Update All") { apply(pending.map(\.swiplet.id)) }
                        .disabled(pending.contains { swiplets.updating.contains($0.swiplet.id) })
                }
                Section("Updatable Swiplets") {
                    ForEach(pending, id: \.swiplet.id, content: row)
                }
            }
            .navigationTitle("Updates")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    CloseButton { dismiss() }
                }
            }
            .onChange(of: pending.isEmpty) {
                if pending.isEmpty {
                    dismiss()
                }
            }
            .alert($alert)
        }
    }

    private func row(_ update: SwipletUpdate) -> some View {
        HStack(spacing: 12) {
            SwipletIconImage(icon: swiplets.icons[update.swiplet.id] ?? .default, size: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: swiplets.titles[update.swiplet.id] ?? update.swiplet.id)
                    .lineLimit(1)
                if !update.swiplet.version.isEmpty {
                    SwipletVersionChange(installed: swiplets.versions[update.swiplet.id] ?? "", available: update.swiplet.version)
                }
            }
            Spacer()
            if swiplets.updating.contains(update.swiplet.id) {
                SwipletDownloadIndicator(progress: swiplets.downloadProgress[update.swiplet.id])
            } else {
                Button("Update") { apply([update.swiplet.id]) }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.capsule)
            }
        }
    }

    private func apply(_ ids: [String]) {
        Task {
            do {
                try await swiplets.applyUpdates(ids)
            } catch {
                alert = AlertState(String(localized: "Can’t Update the Swiplet"), error: error)
            }
        }
    }
}

struct SwipletVersionChange: View {
    let installed: String
    let available: String

    var body: some View {
        Group {
            if installed.isEmpty || installed == available {
                Text(verbatim: available)
            } else {
                HStack(spacing: 4) {
                    Text(verbatim: installed)
                    Image(systemName: "arrow.forward")
                        .imageScale(.small)
                    Text(verbatim: available)
                }
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
    }
}

struct SwipletDownloadIndicator: View {
    let progress: SwipletDownloadProgress?

    var body: some View {
        HStack(spacing: 8) {
            if let progress {
                let received = progress.received.formatted(.byteCount(style: .file))
                Text(verbatim: progress.total > 0 ? "\(received) / \(progress.total.formatted(.byteCount(style: .file)))" : received)
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
            ProgressView()
        }
    }
}
