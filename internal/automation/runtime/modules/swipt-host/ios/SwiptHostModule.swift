import ExpoModulesCore

public enum SwiptHostWebResult: Sendable {
  case data(Data)
  case json(String)
  case failure(String)
}

public protocol SwiptHostWeb: AnyObject, Sendable {
  func start(_ id: String, name: String, arguments: String, completion: @escaping @Sendable (SwiptHostWebResult) -> Void)
  func cancel(_ id: String)
  func put(_ data: Data) -> String
  func release(_ id: String, reason: String)
  func openFile(root: String, path: String) -> String
  func compression(format: String, mode: String) throws -> [String]
  func random(_ length: Int) throws -> Data
  func digest(algorithm: String, data: Data) throws -> Data
  func decodeText(_ data: Data, fatal: Bool) -> (text: String, valid: Bool, restored: Int)
  func close()
}

@MainActor
public protocol SwiptHostDelegate: AnyObject, Sendable {
  nonisolated var swiplet: String { get }
  func call(_ id: String, action: String, input: String, value: @escaping @Sendable (String) -> Void) async throws -> String
  func cancelCall(_ id: String)
  func setServiceEnabled(_ enabled: Bool) async throws
  func searchPlaces(_ query: String) async throws -> String
  func presentDialog(_ id: String, request: String) async throws -> String?
  func withdrawDialog(_ id: String)
  func surfaceDidMount(_ surface: String)
  func exit()
  func log(level: String, message: String)
  func fail(surface: String, message: String)
  func report(message: String)
  nonisolated func storeBlob(_ data: Data, id: String)
  nonisolated func removeBlob(_ id: String)
}

public final class SwiptHostModule: Module {
  nonisolated(unsafe) public static var database: (@Sendable (_ client: String, _ swiplet: String, _ request: String) throws -> String)?
  nonisolated(unsafe) public static var closeDatabases: (@Sendable (_ client: String) -> Void)?
  nonisolated(unsafe) public static var openWeb: (@Sendable (_ swiplet: String) throws -> any SwiptHostWeb)?
  private static let databaseQueue = DispatchQueue(label: "swipt.sqlite", qos: .userInitiated, attributes: .concurrent)

  private static let lock = NSLock()
  private static let modules = NSHashTable<SwiptHostModule>.weakObjects()
  private static let delegates = NSMapTable<AppContext, AnyObject>.weakToWeakObjects()

  private let client = UUID().uuidString
  private let webLock = NSLock()
  private var web: (any SwiptHostWeb)?
  private var webClosed = false

  public static func bind(_ appContext: AppContext, to delegate: any SwiptHostDelegate) {
    lock.lock()
    delegates.setObject(delegate, forKey: appContext)
    lock.unlock()
  }

  public static func unbind(_ delegate: any SwiptHostDelegate) {
    lock.lock()
    let unbound = (delegates.keyEnumerator().allObjects as! [AppContext]).filter { delegates.object(forKey: $0) === delegate }
    for appContext in unbound {
      delegates.removeObject(forKey: appContext)
    }
    let modules = modules.allObjects
    lock.unlock()
    for module in modules where unbound.contains(where: { $0 === module.appContext }) {
      module.closeWeb()
    }
  }

  public static func send(_ event: String, to swiplet: String, _ body: [String: Any?]) {
    lock.lock()
    let modules = modules.allObjects
    lock.unlock()
    for module in modules where module.delegate()?.swiplet == swiplet {
      module.sendEvent(event, body)
    }
  }

  private func delegate() -> (any SwiptHostDelegate)? {
    guard let appContext else { return nil }
    Self.lock.lock()
    defer { Self.lock.unlock() }
    return Self.delegates.object(forKey: appContext) as? any SwiptHostDelegate
  }

  private func host() throws -> any SwiptHostDelegate {
    guard let delegate = delegate() else {
      throw Exception(name: "SwiptHostUnavailable", description: "Swipt isn't ready for swiplets")
    }
    return delegate
  }

  private func webSession() throws -> any SwiptHostWeb {
    webLock.lock()
    defer { webLock.unlock() }
    if let web {
      return web
    }
    guard !webClosed, let open = Self.openWeb else {
      throw Exception(name: "SwiptHostUnavailable", description: "Swipt isn't ready for swiplets")
    }
    let session = try open(try host().swiplet)
    web = session
    return session
  }

  private func openedWeb() -> (any SwiptHostWeb)? {
    webLock.lock()
    defer { webLock.unlock() }
    return web
  }

  private func closeWeb() {
    webLock.lock()
    let session = web
    web = nil
    webClosed = true
    webLock.unlock()
    session?.close()
  }

  private func database(_ request: String) throws -> String {
    guard let database = Self.database else {
      throw Exception(name: "SwiptHostUnavailable", description: "Swipt isn't ready for swiplets")
    }
    return try database(client, try host().swiplet, request)
  }

  public func definition() -> ModuleDefinition {
    Name("SwiptHost")

    Events("service", "sqlite", "value")

    OnCreate {
      Self.lock.lock()
      Self.modules.add(self)
      Self.lock.unlock()
    }

    OnDestroy {
      Self.lock.lock()
      Self.modules.remove(self)
      Self.lock.unlock()
      Self.closeDatabases?(self.client)
      self.closeWeb()
    }

    Constant("osVersion") { () -> String in
      let version = ProcessInfo.processInfo.operatingSystemVersion
      return "\(version.majorVersion).\(version.minorVersion).\(version.patchVersion)"
    }

    Function("languages") { () -> [String] in
      Locale.preferredLanguages
    }

    AsyncFunction("webStart") { (id: String, name: String, arguments: String) async throws -> [String: Any] in
      let session = try self.webSession()
      let result = await withCheckedContinuation { continuation in
        session.start(id, name: name, arguments: arguments) { continuation.resume(returning: $0) }
      }
      switch result {
      case .data(let data):
        return ["data": data]
      case .json(let json):
        return ["json": json]
      case .failure(let failure):
        return ["failure": failure]
      }
    }

    Function("webCancel") { (id: String) in
      self.openedWeb()?.cancel(id)
    }

    Function("webPut") { (data: Data) throws -> String in
      try self.webSession().put(data)
    }

    Function("webRelease") { (id: String, reason: String) in
      self.openedWeb()?.release(id, reason: reason)
    }

    Function("webOpenFile") { (root: String, path: String) throws -> String in
      try self.webSession().openFile(root: root, path: path)
    }

    Function("webCompression") { (format: String, mode: String) throws -> [String] in
      try self.webSession().compression(format: format, mode: mode)
    }

    Function("webRandom") { (length: Double) throws -> Data in
      guard let length = Int(exactly: length) else {
        throw Exception(name: "TypeError", description: "the length must be an integer")
      }
      return try self.webSession().random(length)
    }

    Function("webDigest") { (algorithm: String, data: Data) throws -> Data in
      try self.webSession().digest(algorithm: algorithm, data: data)
    }

    Function("webDecodeText") { (data: Data, fatal: Bool) throws -> [String: Any] in
      let decoded = try self.webSession().decodeText(data, fatal: fatal)
      return decoded.valid ? ["text": decoded.text] : ["restored": decoded.restored]
    }

    Function("publishObjectURL") { (url: String, data: Data?) throws in
      guard url.hasPrefix("blob:") else {
        throw Exception(name: "TypeError", description: "the URL must be a blob URL")
      }
      let id = String(url.dropFirst("blob:".count))
      if let data {
        try self.host().storeBlob(data, id: id)
      } else {
        try self.host().removeBlob(id)
      }
    }

    AsyncFunction("call") { @MainActor (id: String, action: String, input: String) async throws -> String in
      defer { self.sendEvent("value", ["call": id]) }
      return try await self.host().call(id, action: action, input: input) { [weak self] value in
        self?.sendEvent("value", ["call": id, "value": value])
      }
    }

    AsyncFunction("cancelCall") { @MainActor (id: String) async in
      guard let delegate = self.delegate() else { return }
      delegate.cancelCall(id)
    }

    AsyncFunction("setServiceEnabled") { @MainActor (enabled: Bool) async throws in
      try await self.host().setServiceEnabled(enabled)
    }

    AsyncFunction("searchPlaces") { @MainActor (query: String) async throws -> String in
      try await self.host().searchPlaces(query)
    }

    AsyncFunction("presentDialog") { @MainActor (id: String, request: String) async throws -> String? in
      try await self.host().presentDialog(id, request: request)
    }

    AsyncFunction("withdrawDialog") { @MainActor (id: String) async in
      guard let delegate = self.delegate() else { return }
      delegate.withdrawDialog(id)
    }

    Function("mounted") { (surface: String) in
      guard let delegate = self.delegate() else { return }
      Task { @MainActor in
        delegate.surfaceDidMount(surface)
      }
    }

    Function("exit") {
      guard let delegate = self.delegate() else { return }
      Task { @MainActor in
        delegate.exit()
      }
    }

    Function("sqlite") { (request: String) throws -> String in
      try self.database(request)
    }

    AsyncFunction("sqliteAsync") { (request: String) throws -> String in
      try self.database(request)
    }
    .runOnQueue(Self.databaseQueue)

    Function("preferredLocalizations") { (localizations: [String]) -> [String] in
      Bundle.preferredLocalizations(from: localizations, forPreferences: Locale.preferredLanguages)
    }

    Function("log") { (level: String, message: String) in
      guard let delegate = self.delegate() else { return }
      Task { @MainActor in
        delegate.log(level: level, message: message)
      }
    }

    Function("fail") { (surface: String, message: String) in
      guard let delegate = self.delegate() else { return }
      Task { @MainActor in
        delegate.fail(surface: surface, message: message)
      }
    }

    Function("report") { (message: String) in
      guard let delegate = self.delegate() else { return }
      Task { @MainActor in
        delegate.report(message: message)
      }
    }
  }
}
