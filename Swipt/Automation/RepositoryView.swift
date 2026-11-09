import Perception
import SwiftUI

@available(iOS 17.4, *)
struct RepositoryView: View {
    let url: String

    @State private var isLoaded = false
    @State private var alert: AlertState?

    private let swiplets = SwipletStore.shared

    var body: some View {
        WithPerceptionTracking {
            let repository = swiplets.repository(url)
            Form {
                if let repository, !repository.swiplets.isEmpty {
                    Section {
                        ForEach(repository.swiplets) { swiplet in
                            row(swiplet, in: repository)
                        }
                    }
                } else if isLoaded {
                    Text("No Swiplets in This Repository")
                        .foregroundStyle(.secondary)
                }
            }
            .overlay {
                if !isLoaded, repository?.swiplets.isEmpty ?? true {
                    ProgressView()
                }
            }
            .navigationTitle(Text(verbatim: repository?.title ?? ""))
            .refreshable { await refresh(force: true) }
            .task {
                await refresh(force: false)
                isLoaded = true
            }
            .alert($alert)
        }
    }

    private func refresh(force: Bool) async {
        do {
            try await swiplets.refreshRepository(url, force: force)
        } catch {
            alert = AlertState(String(localized: "Couldn’t Refresh the Repository"), error: error)
        }
    }

    private func row(_ swiplet: RepositorySwiplet, in repository: Repository) -> some View {
        HStack(spacing: 12) {
            SwipletIconImage(icon: swiplet.icon, size: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: swiplet.title)
                    .lineLimit(1)
                if !swiplet.description.isEmpty {
                    Text(verbatim: swiplet.description)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                if !swiplet.version.isEmpty {
                    let isUpdatable = swiplets.update(of: swiplet.id)?.repository.url == repository.url
                    SwipletVersionChange(installed: isUpdatable ? swiplets.versions[swiplet.id] ?? "" : "", available: swiplet.version)
                }
            }
            Spacer()
            trailing(swiplet, in: repository)
        }
    }

    @ViewBuilder
    private func trailing(_ swiplet: RepositorySwiplet, in repository: Repository) -> some View {
        if swiplets.updating.contains(swiplet.id) {
            SwipletDownloadIndicator(progress: swiplets.downloadProgress[swiplet.id])
        } else if swiplets.repositoryURLs[swiplet.id] != repository.url {
            Button("Add") { install(swiplet, from: repository) }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
        } else if swiplets.update(of: swiplet.id) != nil {
            Button("Update") { install(swiplet, from: repository) }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
        } else {
            Image(systemName: "checkmark")
                .foregroundStyle(.secondary)
        }
    }

    private func install(_ swiplet: RepositorySwiplet, from repository: Repository) {
        Task {
            do {
                try await swiplets.install(swiplet, from: repository)
            } catch {
                alert = AlertState(String(localized: "Can’t Add the Swiplet"), error: error)
            }
        }
    }
}

@available(iOS 17.4, *)
struct AddRepositoryView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var url = ""
    @State private var isAdding = false
    @State private var alert: AlertState?

    var body: some View {
        Form {
            TextField("URL", text: $url)
                .keyboardType(.URL)
                .textContentType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.done)
                .onSubmit(add)
        }
        .disabled(isAdding)
        .navigationTitle("Add Repository")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isAdding {
                    ProgressView()
                } else {
                    Button("Add", action: add)
                        .disabled(url.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
        .alert($alert)
    }

    private func add() {
        guard !isAdding, !url.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        isAdding = true
        Task {
            do {
                try await SwipletStore.shared.addRepository(url)
                dismiss()
            } catch {
                alert = AlertState(String(localized: "Can’t Add the Repository"), error: error)
            }
            isAdding = false
        }
    }
}
