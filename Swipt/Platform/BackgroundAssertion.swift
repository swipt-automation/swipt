import Foundation
import ObjectiveC
import UIKit
import os

final class BackgroundAssertion {
    private(set) var isActive = false {
        didSet {
            guard isActive != oldValue else { return }
            onActiveChange?()
        }
    }

    var onActiveChange: (() -> Void)?
    var onFailure: ((any Error) -> Void)?

    nonisolated private static let logger = Logger(
        subsystem: Bundle.main.bundleIdentifier ?? "Swipt", category: "background")

    static let isSupported: Bool = {
        do {
            try Assertion().invalidate()
            return true
        } catch {
            BackgroundAssertion.logger.info("UAFAssetAccess is unavailable: \(error, privacy: .public)")
            return false
        }
    }()

    private var assertion: Assertion?
    private var renewal: Timer?
    private var observer: (any NSObjectProtocol)?

    func start() {
        guard observer == nil else { return }
        observer = NotificationCenter.default.addObserver(
            forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.renew() }
        }
        renew()
    }

    func stop() {
        if let observer {
            NotificationCenter.default.removeObserver(observer)
        }
        observer = nil
        renewal?.invalidate()
        renewal = nil
        assertion?.invalidate()
        assertion = nil
        isActive = false
    }

    private func renew() {
        guard observer != nil else { return }
        renewal?.invalidate()
        let delay: TimeInterval
        do {
            let next = try Assertion()
            assertion?.invalidate()
            assertion = next
            isActive = true
            // UAFAssetAccess expires 300 seconds after each assertion is created (RunningBoard 24C5037i,
            // LifecyclePolicy/DomainAttributes/com.apple.UnifiedAssetFramework.plist: StartPolicy Fixed, InvalidationDuration 300).
            delay = 240
        } catch {
            Self.logger.error("Couldn't acquire the background assertion: \(error, privacy: .public)")
            isActive = false
            onFailure?(error)
            delay = 15
        }
        let timer = Timer(timeInterval: delay, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.renew() }
        }
        timer.tolerance = 5
        renewal = timer
        RunLoop.main.add(timer, forMode: .common)
    }

    private final class Assertion {
        private typealias Invalidate = @convention(c) (AnyObject, Selector) -> Void

        private static let framework = dlopen(
            "/System/Library/PrivateFrameworks/RunningBoardServices.framework/RunningBoardServices",
            RTLD_NOW)
        private let object: AnyObject
        private let invalidateSelector: Selector
        private let invalidateMethod: Invalidate

        init() throws {
            guard Self.framework != nil,
                let targetClass = NSClassFromString("RBSTarget"),
                let attributeClass = NSClassFromString("RBSDomainAttribute"),
                let assertionClass = NSClassFromString("RBSAssertion")
            else { throw MessageError("RunningBoardServices is unavailable") }

            typealias Target = @convention(c) (AnyObject, Selector, Int32) -> Unmanaged<AnyObject>?
            typealias Attribute =
                @convention(c) (AnyObject, Selector, AnyObject, AnyObject) -> Unmanaged<AnyObject>?
            typealias Allocate = @convention(c) (AnyObject, Selector) -> Unmanaged<AnyObject>?
            typealias Initialize =
                @convention(c) (AnyObject, Selector, AnyObject, AnyObject, AnyObject) -> Unmanaged<
                    AnyObject
                >?
            typealias Acquire =
                @convention(c) (AnyObject, Selector, AutoreleasingUnsafeMutablePointer<NSError?>?) -> Bool

            let (targetSelector, targetIMP) = try Self.method(
                targetClass, "targetWithPid:", classMethod: true)
            let (attributeSelector, attributeIMP) = try Self.method(
                attributeClass, "attributeWithDomain:name:", classMethod: true)
            let (allocSelector, allocIMP) = try Self.method(assertionClass, "alloc", classMethod: true)
            let (initSelector, initIMP) = try Self.method(
                assertionClass, "initWithExplanation:target:attributes:")
            let (acquireSelector, acquireIMP) = try Self.method(assertionClass, "acquireWithError:")
            let (invalidateSelector, invalidateIMP) = try Self.method(assertionClass, "invalidate")

            guard
                let target = unsafeBitCast(targetIMP, to: Target.self)(
                    targetClass as AnyObject, targetSelector, getpid())?.takeUnretainedValue(),
                let attribute = unsafeBitCast(attributeIMP, to: Attribute.self)(
                    attributeClass as AnyObject, attributeSelector,
                    "com.apple.UnifiedAssetFramework" as NSString, "UAFAssetAccess" as NSString)?
                    .takeUnretainedValue(),
                let allocated = unsafeBitCast(allocIMP, to: Allocate.self)(
                    assertionClass as AnyObject, allocSelector)?.takeUnretainedValue(),
                let object = unsafeBitCast(initIMP, to: Initialize.self)(
                    allocated, initSelector, "Swipt background service" as NSString, target,
                    [attribute] as NSArray)?.takeRetainedValue()
            else { throw MessageError("RunningBoardServices returned no assertion") }

            var failure: NSError?
            guard unsafeBitCast(acquireIMP, to: Acquire.self)(object, acquireSelector, &failure) else {
                throw failure ?? MessageError("RBSAssertion.acquireWithError: returned false")
            }
            self.object = object
            self.invalidateSelector = invalidateSelector
            invalidateMethod = unsafeBitCast(invalidateIMP, to: Invalidate.self)
        }

        func invalidate() {
            invalidateMethod(object, invalidateSelector)
        }

        private static func method(_ type: AnyClass, _ name: String, classMethod: Bool = false) throws
            -> (Selector, IMP)
        {
            let selector = NSSelectorFromString(name)
            let method =
                classMethod ? class_getClassMethod(type, selector) : class_getInstanceMethod(type, selector)
            guard let method else { throw MessageError("Missing \(NSStringFromClass(type)).\(name)") }
            return (selector, method_getImplementation(method))
        }
    }
}
