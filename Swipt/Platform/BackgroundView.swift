import Perception
import SwiftUI

struct BackgroundView: View {
    @Perception.Bindable private var settings = BackgroundSettings.shared

    var body: some View {
        WithPerceptionTracking {
            Form {
                Section {
                    Toggle("Enabled", isOn: $settings.isEnabled)
                } footer: {
                    Text("Swipt needs to stay running in the background to keep automating this device.")
                }
                if settings.isEnabled {
                    Section("Keep Running With") {
                        Picker("Keep Running With", selection: $settings.mode) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("Developer Service")
                                Text("Requires iOS 27.2.")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                            .selectionDisabledCompat(!BackgroundAssertion.isSupported)
                            .tag(BackgroundMode.system)
                            VStack(alignment: .leading, spacing: 2) {
                                Text("Location Service")
                                Text("The status bar shows that location is in use.")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                            .tag(BackgroundMode.location)
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    }
                }
            }
            .navigationTitle("Background Service")
        }
    }
}
