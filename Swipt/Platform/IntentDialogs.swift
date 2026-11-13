import AppIntents

@available(iOS 17.4, *)
extension AppIntent {
    func present(_ request: DialogRequest, value parameter: IntentParameter<String?>) async throws -> DialogAnswer {
        let dialog = request.prompt.map { IntentDialog(.verbatim($0)) }
        switch request {
        case .choice(_, let options):
            guard #available(iOS 26, *) else {
                let choices = options.indices.filter { options[$0].style != .cancel }
                guard !choices.isEmpty else { throw CancellationError() }
                let chosen = try await parameter.requestDisambiguation(among: choices.map { options[$0].title }, dialog: dialog)
                guard let index = choices.first(where: { options[$0].title == chosen }) else { throw CancellationError() }
                return .index(index)
            }
            let chosen = try await requestChoice(between: DialogRequest.Choice.intentOptions(options), dialog: dialog)
            guard let index = options.firstIndex(where: { $0.intentOption == chosen }) else { throw CancellationError() }
            return .index(index)
        case .confirmation(let prompt, let action):
            let actionName: ConfirmationActionName =
                switch action {
                case .named(let name): name.intentName
                case .custom(let accept, let deny, let destructive):
                    .custom(acceptLabel: .verbatim(accept), acceptAlternatives: [], denyLabel: .verbatim(deny), denyAlternatives: [], destructive: destructive)
                }
            if #available(iOS 18, *) {
                try await requestConfirmation(actionName: actionName, dialog: IntentDialog(.verbatim(prompt)))
            } else {
                try await requestConfirmation(result: .result(dialog: IntentDialog(.verbatim(prompt))), confirmationActionName: actionName)
            }
            return .none
        // Shortcuts asks only for parameters its editor shows
        // (-[WFSequentialParameterInputProvider askForParameterIfAvailable], iOS 27 24A446).
        case .value:
            return try await ScriptDialogs.shared.present(request)
        case .disambiguation(_, let values):
            let chosen = try await parameter.requestDisambiguation(among: values, dialog: dialog)
            return .index(values.firstIndex(of: chosen) ?? 0)
        case .valueConfirmation(_, let value):
            return .confirmed(try await parameter.requestConfirmation(for: value, dialog: dialog))
        }
    }
}

@available(iOS 26, *)
extension DialogRequest.Choice {
    nonisolated var intentOption: IntentChoiceOption {
        let style: IntentChoiceOption.Style =
            switch style {
            case .default: .default
            case .destructive: .destructive
            case .cancel: .cancel
            }
        return IntentChoiceOption(title: .verbatim(title), style: style)
    }

    nonisolated static func intentOptions(_ choices: [Self]) -> [IntentChoiceOption] {
        choices.map(\.intentOption)
    }
}

@available(iOS 17.4, *)
extension DialogRequest.ActionName {
    var intentName: ConfirmationActionName {
        switch self {
        case .add: .add
        case .addData: .addData
        case .book: .book
        case .buy: .buy
        case .call: .call
        case .checkIn: .checkIn
        case .continue: .continue
        case .create: .create
        case .do: .do
        case .download: .download
        case .filter: .filter
        case .find: .find
        case .get: .get
        case .go: .go
        case .log: .log
        case .open: .open
        case .order: .order
        case .pay: .pay
        case .play: .play
        case .playSound: .playSound
        case .post: .post
        case .request: .request
        case .run: .run
        case .search: .search
        case .send: .send
        case .set: .set
        case .share: .share
        case .start: .start
        case .startNavigation: .startNavigation
        case .toggle: .toggle
        case .turnOff: .turnOff
        case .turnOn: .turnOn
        case .view: .view
        }
    }
}

extension LocalizedStringResource {
    nonisolated static func verbatim(_ string: String) -> LocalizedStringResource {
        LocalizedStringResource(String.LocalizationValue(string), table: "Verbatim")
    }
}
