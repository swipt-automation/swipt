import AuthenticationServices
import LibSwipt
import Perception
import SwiftUI
import UIKit

struct DeveloperView: View {
    var body: some View {
        Form {
            Section {
                NavigationLink {
                    LocalNetworkView()
                } label: {
                    PublishRow(icon: "wifi", title: Text("Local Network"), description: Text("Provides the Swipt API and MCP to the local network."))
                }
                NavigationLink {
                    CloudflareView()
                } label: {
                    PublishRow(
                        icon: "cloud", title: Text(verbatim: "Cloudflare"),
                        description: Text("Securely provides the Swipt API and MCP to the internet through Cloudflare."))
                }
                NavigationLink {
                    TailscaleView()
                } label: {
                    PublishRow(
                        icon: "point.3.connected.trianglepath.dotted", title: Text(verbatim: "Tailscale"),
                        description: Text("Provides the Swipt API and MCP to your Tailscale network."))
                }
            } header: {
                Text(verbatim: "API")
            }
        }
        .navigationTitle("Developer")
    }
}

private struct PublishRow: View {
    let icon: String
    let title: Text
    let description: Text

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                title
                description
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: icon)
                .font(.title2)
        }
        .padding(.vertical, 6)
    }
}

private struct LocalNetworkView: View {
    @State private var localHostName = LocalHostName()
    @State private var host = ""

    @Perception.Bindable private var settings = DeveloperSettings.shared

    var body: some View {
        WithPerceptionTracking {
            Form {
                Section {
                    Toggle("Enabled", isOn: $settings.isLocalNetworkEnabled)
                }
                Section("Configuration") {
                    ValueRow(title: "Port", value: String(settings.localPort), keyboard: .numberPad) { text in
                        if let port = Int(text), DeveloperSettings.portRange.contains(port) {
                            settings.localPort = port
                        }
                    }
                }
                Section("Authentication") {
                    Toggle("Enabled", isOn: $settings.localAuthenticates)
                    if settings.localAuthenticates {
                        ValueRow(
                            title: "Token", value: settings.localToken, isSecret: true,
                            random: ("Random Token", { DeveloperSettings.randomHex(32) })
                        ) { text in
                            let token = text.trimmingCharacters(in: .whitespacesAndNewlines)
                            if !token.isEmpty {
                                settings.localToken = token
                            }
                        }
                    }
                }
                let hosts = localHostName.hosts
                if !hosts.isEmpty {
                    let selected = hosts.contains(host) ? host : hosts[0]
                    let address = "\(selected):\(settings.localPort)"
                    let token = settings.localAuthenticates ? settings.localToken : nil
                    UsageSection(title: Text(verbatim: "Swipt API")) {
                        Picker("Host", selection: Binding(get: { selected }, set: { host = $0 })) {
                            ForEach(hosts, id: \.self) { host in
                                Text(verbatim: host).tag(host)
                            }
                        }
                        .pickerStyle(.menu)
                    } items: { revealed in
                        apiUsage(scheme: "http", address: address, token: token, revealed: revealed)
                    }
                    UsageSection(title: Text(verbatim: "MCP")) {
                    } items: { revealed in
                        mcpUsage(scheme: "http", address: address, token: token, revealed: revealed)
                    }
                }
            }
            .navigationTitle("Local Network")
            .onAppear {
                localHostName.start()
            }
            .onDisappear {
                localHostName.stop()
            }
        }
    }
}

private struct CloudflareView: View {
    @Perception.Bindable private var settings = DeveloperSettings.shared

    var body: some View {
        WithPerceptionTracking {
            Form {
                Section {
                    Toggle("Enabled", isOn: $settings.isCloudflareEnabled)
                        .disabled(!settings.isCloudflareEnabled && !settings.isCloudflareComplete)
                }
                Section("Configuration") {
                    NavigationLink {
                        CloudflareModeView()
                    } label: {
                        LabeledContentCompat("Mode") {
                            Text(verbatim: settings.cloudflareMode.name)
                        }
                    }
                    ValueRow(
                        title: "Prefix", value: settings.cloudflarePath, keyboard: .URL,
                        random: ("Random Path", { "/\(DeveloperSettings.randomHex(16))" })
                    ) { text in
                        var path = text.trimmingCharacters(in: .whitespacesAndNewlines)
                        while path.hasSuffix("/") {
                            path.removeLast()
                        }
                        settings.cloudflarePath = path.hasPrefix("/") ? path : "/" + path
                    }
                }
                .disabled(settings.isCloudflareEnabled)
                Section("Authentication") {
                    ValueRow(
                        title: "Token", value: settings.cloudflareToken, isSecret: true,
                        random: ("Random Token", { DeveloperSettings.randomHex(32) })
                    ) { text in
                        let token = text.trimmingCharacters(in: .whitespacesAndNewlines)
                        if !token.isEmpty {
                            settings.cloudflareToken = token
                        }
                    }
                }
                let hostname = settings.cloudflarePublicHostname
                if !hostname.isEmpty {
                    let address = { (revealed: Bool) in "\(maskedHost(hostname, revealed: revealed))\(settings.cloudflarePathPrefix)" }
                    UsageSection(title: Text(verbatim: "Swipt API")) {
                    } items: { revealed in
                        apiUsage(scheme: "https", address: address(revealed), token: settings.cloudflareToken, revealed: revealed)
                    }
                    UsageSection(title: Text(verbatim: "MCP")) {
                    } items: { revealed in
                        mcpUsage(scheme: "https", address: address(revealed), token: settings.cloudflareToken, revealed: revealed)
                    }
                }
            }
            .navigationTitle(Text(verbatim: "Cloudflare"))
        }
    }
}

private struct CloudflareModeView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var mode: CloudflareMode
    @State private var account: String
    @State private var accountName: String
    @State private var zone: String
    @State private var subdomain: String
    @State private var isLoggingIn = false
    @State private var alert: AlertState?

    init() {
        let settings = DeveloperSettings.shared
        _mode = State(initialValue: settings.cloudflareMode)
        _account = State(initialValue: settings.cloudflareAccount)
        _accountName = State(initialValue: settings.cloudflareAccountName)
        _zone = State(initialValue: settings.cloudflareZone)
        let suffix = ".\(settings.cloudflareZone)"
        _subdomain = State(initialValue: settings.cloudflareHostname.hasSuffix(suffix) ? String(settings.cloudflareHostname.dropLast(suffix.count)) : "")
    }

    var body: some View {
        Form {
            Section("Mode") {
                Picker("Mode", selection: $mode) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: CloudflareMode.quickTunnel.name)
                        Text("Uses a domain provided by Cloudflare, which is released when turned off.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    .tag(CloudflareMode.quickTunnel)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: CloudflareMode.tunnel.name)
                        Text("Uses your own account and domain.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    .tag(CloudflareMode.tunnel)
                }
                .pickerStyle(.inline)
                .labelsHidden()
            }
            if mode == .tunnel {
                Section {
                    if account.isEmpty {
                        Button("Log In", action: logIn)
                            .disabled(isLoggingIn)
                    } else {
                        if !accountName.isEmpty {
                            LabeledContentCompat("Name") {
                                Text(verbatim: accountName)
                            }
                        }
                        Button("Reauthorize", action: logIn)
                            .disabled(isLoggingIn)
                    }
                } header: {
                    Text("Account")
                }
                if !account.isEmpty {
                    Section("Domain") {
                        LabeledContentCompat("Domain") {
                            Text(verbatim: zone)
                        }
                        ValueRow(title: "Subdomain", value: subdomain, keyboard: .URL) { text in
                            var label = text.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
                            if label.hasSuffix(".\(zone)") {
                                label.removeLast(zone.count + 1)
                            }
                            subdomain = label.trimmingCharacters(in: CharacterSet(charactersIn: "."))
                        }
                    }
                }
            }
        }
        .navigationTitle(Text(verbatim: "Cloudflare"))
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Save", action: save)
                    .disabled(!isComplete)
            }
        }
        .alert($alert)
    }

    private var isComplete: Bool {
        switch mode {
        case .quickTunnel: true
        case .tunnel: !account.isEmpty && !subdomain.isEmpty
        }
    }

    private func logIn() {
        guard let login = LibSwiptNewCloudflareLogin() else { return }
        let box = UncheckedSendable(value: login)
        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                guard let result = try await WebLogin().run(url: login.loginURL(), wait: { try box.value.complete() }, cancel: { box.value.cancel() }) else {
                    return
                }
                account = result.credential
                accountName = result.accountName
                zone = result.zoneName
                if subdomain.isEmpty {
                    subdomain = Platform.shared.hostLabel.lowercased()
                }
            } catch {
                alert = AlertState(String(localized: "Can’t Log In"), error: error)
            }
        }
    }

    private func save() {
        let settings = DeveloperSettings.shared
        if account != settings.cloudflareAccount {
            settings.cloudflareNamedTunnel = ""
        }
        settings.cloudflareAccount = account
        settings.cloudflareAccountName = accountName
        settings.cloudflareZone = zone
        settings.cloudflareHostname = account.isEmpty ? "" : "\(subdomain).\(zone)"
        settings.cloudflareMode = mode
        dismiss()
    }
}

private struct TailscaleView: View {
    @Perception.Bindable private var settings = DeveloperSettings.shared
    @State private var host = ""
    @State private var isWorking = false
    @State private var alert: AlertState?

    var body: some View {
        WithPerceptionTracking {
            let tailscale = Platform.shared.tailscale
            let status = Platform.shared.tailscaleStatus
            Form {
                Section {
                    Toggle("Enabled", isOn: $settings.isTailscaleEnabled)
                        .disabled(!settings.isTailscaleEnabled && status?.isLoggedIn != true)
                }
                Section {
                    switch status?.state {
                    case .needsLogin:
                        Button("Log In") { logIn(tailscale) }
                            .disabled(isWorking || tailscale == nil)
                    case .stopped, .needsMachineAuth, .running:
                        if let status {
                            LabeledContentCompat("Name") {
                                Text(verbatim: status.loginName)
                            }
                            if !status.tailnet.isEmpty, status.tailnet != status.loginName {
                                LabeledContentCompat("Network") {
                                    Text(verbatim: status.tailnet)
                                }
                            }
                        }
                        Button("Log Out", role: .destructive) { logOut(tailscale) }
                            .disabled(isWorking || tailscale == nil)
                    case .starting, nil:
                        LabeledContentCompat("Connecting…") {
                            ProgressView()
                        }
                    }
                } header: {
                    Text("Account")
                } footer: {
                    if status?.state == .needsMachineAuth {
                        Text("Waiting for an admin of your Tailscale network to approve this device.")
                    }
                }
                Section("Authentication") {
                    Toggle("Enabled", isOn: $settings.tailscaleAuthenticates)
                    if settings.tailscaleAuthenticates {
                        ValueRow(
                            title: "Token", value: settings.tailscaleToken, isSecret: true,
                            random: ("Random Token", { DeveloperSettings.randomHex(32) })
                        ) { text in
                            let token = text.trimmingCharacters(in: .whitespacesAndNewlines)
                            if !token.isEmpty {
                                settings.tailscaleToken = token
                            }
                        }
                    }
                }
                if let status, status.state == .running {
                    let hosts = ([status.dnsName] + status.addresses.map { $0.contains(":") ? "[\($0)]" : $0 }).filter { !$0.isEmpty }
                    if !hosts.isEmpty {
                        let selected = hosts.contains(host) ? host : hosts[0]
                        let token = settings.tailscaleAuthenticates ? settings.tailscaleToken : nil
                        UsageSection(title: Text(verbatim: "Swipt API")) {
                            Picker("Host", selection: Binding(get: { selected }, set: { host = $0 })) {
                                ForEach(hosts, id: \.self) { host in
                                    Text(verbatim: host).tag(host)
                                }
                            }
                            .pickerStyle(.menu)
                        } items: { revealed in
                            apiUsage(scheme: "http", address: selected, token: token, revealed: revealed)
                        }
                        UsageSection(title: Text(verbatim: "MCP")) {
                        } items: { revealed in
                            mcpUsage(scheme: "http", address: selected, token: token, revealed: revealed)
                        }
                    }
                }
            }
            .navigationTitle(Text(verbatim: "Tailscale"))
            .alert($alert)
        }
    }

    private func logIn(_ tailscale: LibSwiptTailscale?) {
        guard let tailscale else { return }
        let box = UncheckedSendable(value: tailscale)
        isWorking = true
        Task {
            defer { isWorking = false }
            do {
                let login = try await blocking { UncheckedSendable(value: try box.value.login()) }.value
                let loginBox = UncheckedSendable(value: login)
                _ = try await WebLogin().run(url: login.loginURL(), wait: { try loginBox.value.complete() }, cancel: { loginBox.value.cancel() })
            } catch {
                alert = AlertState(String(localized: "Can’t Log In"), error: error)
            }
        }
    }

    private func logOut(_ tailscale: LibSwiptTailscale?) {
        guard let tailscale else { return }
        let box = UncheckedSendable(value: tailscale)
        isWorking = true
        Task {
            defer { isWorking = false }
            do {
                try await blocking { try box.value.logout() }
            } catch {
                alert = AlertState(String(localized: "Can’t Log Out"), error: error)
            }
        }
    }
}

private func maskedToken(_ token: String) -> String {
    String(repeating: "•", count: min(token.count, 3))
}

private func maskedHost(_ host: String, revealed: Bool) -> String {
    revealed || !host.hasSuffix(".trycloudflare.com") ? host : "***" + host.drop { $0 != "." }
}

private func apiURL(scheme: String, address: String, token: String?, revealed: Bool) -> String {
    let unreserved = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")
    let credentials = token.map { (revealed ? $0.addingPercentEncoding(withAllowedCharacters: unreserved)! : maskedToken($0)) + "@" } ?? ""
    return "\(scheme)://\(credentials)\(address)"
}

private func apiUsage(scheme: String, address: String, token: String?, revealed: Bool) -> [UsageItem] {
    let url = apiURL(scheme: scheme, address: address, token: token, revealed: revealed)
    return [
        UsageItem(label: String(localized: "URL"), text: url),
        UsageItem(label: "Swipt CLI", text: "swipt device add \(url)"),
    ]
}

private func mcpUsage(scheme: String, address: String, token: String?, revealed: Bool) -> [UsageItem] {
    let url = apiURL(scheme: scheme, address: address, token: token, revealed: revealed) + LibSwiptAPIMCPPath
    let name = Platform.shared.hostLabel
    return [
        UsageItem(label: String(localized: "URL"), text: url),
        UsageItem(label: "Claude Code", text: "claude mcp add --transport http \(name) \(url)"),
        UsageItem(label: "Codex", text: "codex mcp add \(name) --url \(url)"),
    ]
}

private struct UsageItem {
    let label: String
    let text: String
}

private struct UsageSection<Leading: View>: View {
    let title: Text
    @ViewBuilder let leading: Leading
    let items: (_ revealed: Bool) -> [UsageItem]
    @State private var isRevealed = false

    var body: some View {
        WithPerceptionTracking {
            let commands = items(true)
            let displays = items(isRevealed)
            Section {
                leading
                ForEach(commands.indices, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(verbatim: commands[index].label)
                        CommandBlock(command: commands[index].text, display: displays[index].text)
                    }
                    .padding(.vertical, 4)
                }
            } header: {
                HStack {
                    title
                    Spacer()
                    Button {
                        isRevealed.toggle()
                    } label: {
                        Image(systemName: isRevealed ? "eye.slash" : "eye")
                    }
                    .accessibilityLabel(isRevealed ? Text("Hide") : Text("Show"))
                }
            }
        }
    }
}

private struct CommandBlock: View {
    let command: String
    let display: String
    @State private var isCopied = false

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(verbatim: display)
                .font(.system(.footnote, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button {
                UIPasteboard.general.string = command
                isCopied = true
                Task {
                    try? await Task.sleep(seconds: 1.5)
                    isCopied = false
                }
            } label: {
                Image(systemName: isCopied ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Copy"))
        }
        .padding(10)
        .background(Color(uiColor: .tertiarySystemFill), in: RoundedRectangle(cornerRadius: 8))
    }
}

private struct ValueRow: View {
    let title: LocalizedStringKey
    let value: String
    var isSecret = false
    var keyboard = UIKeyboardType.default
    var random: (title: LocalizedStringKey, make: () -> String)?
    let save: (String) -> Void
    @State private var text = ""
    @State private var isEditing = false

    var body: some View {
        Button {
            text = value
            isEditing = true
        } label: {
            LabeledContentCompat(title) {
                if value.isEmpty {
                    Text("Required")
                } else {
                    Text(verbatim: isSecret ? maskedToken(value) : value)
                }
            }
        }
        .foregroundStyle(.primary)
        .alert(title, isPresented: $isEditing) {
            TextField(title, text: $text)
                .keyboardType(keyboard)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            if let random {
                Button(random.title) { save(random.make()) }
            }
            Button("Cancel", role: .cancel) {}
            Button("OK") { save(text) }
        }
    }
}

private final class WebLogin: NSObject, ASWebAuthenticationPresentationContextProviding {
    func run<Value>(url: String, wait: @escaping @Sendable () throws -> Value, cancel: @escaping @Sendable () -> Void) async throws -> Value? {
        guard let url = URL(string: url) else {
            cancel()
            return nil
        }
        let dismissed = LockedState(initialState: false)
        let session = ASWebAuthenticationSession(url: url, callbackURLScheme: nil) { @Sendable _, error in
            guard error != nil else { return }
            dismissed.withLock { $0 = true }
            cancel()
        }
        session.presentationContextProvider = self
        session.prefersEphemeralWebBrowserSession = false
        guard session.start() else {
            cancel()
            return nil
        }
        defer { session.cancel() }
        do {
            return try await blocking { UncheckedSendable(value: try wait()) }.value
        } catch {
            if dismissed.withLock({ $0 }) { return nil }
            throw error
        }
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.flatMap(\.windows).first(where: \.isKeyWindow) ?? ASPresentationAnchor()
    }
}
