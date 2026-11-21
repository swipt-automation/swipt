//go:build darwin && cgo

#import "error_darwin.h"

#include <stdatomic.h>

#include "graphics_darwin.h"

@implementation SWCancelToken {
    atomic_bool _cancelled;
    NSLock *_lock;
    uint64_t _next;
    NSMutableDictionary<NSNumber *, dispatch_block_t> *_handlers;
}

- (instancetype)init {
    self = [super init];
    if (self) {
        _lock = [NSLock new];
    }
    return self;
}

- (BOOL)isCancelled {
    return atomic_load_explicit(&_cancelled, memory_order_acquire);
}

- (void)cancel {
    [_lock lock];
    if (atomic_exchange_explicit(&_cancelled, true, memory_order_acq_rel)) {
        [_lock unlock];
        return;
    }
    NSArray<dispatch_block_t> *handlers = _handlers.allValues;
    [_handlers removeAllObjects];
    [_lock unlock];
    for (dispatch_block_t handler in handlers) {
        handler();
    }
}

- (void (^)(void))whenCancelled:(void (^)(void))handler {
    [_lock lock];
    if (atomic_load_explicit(&_cancelled, memory_order_acquire)) {
        [_lock unlock];
        handler();
        return ^{};
    }
    if (_handlers == nil) {
        _handlers = [NSMutableDictionary dictionary];
    }
    NSNumber *key = @(++_next);
    _handlers[key] = [handler copy];
    [_lock unlock];
    return ^{
        [self->_lock lock];
        [self->_handlers removeObjectForKey:key];
        [self->_lock unlock];
    };
}

@end

void *swipt_graphics_token_create(void) {
    @autoreleasepool {
        return (void *)CFBridgingRetain([SWCancelToken new]);
    }
}

void swipt_graphics_token_cancel(void *token) {
    @autoreleasepool {
        [(__bridge SWCancelToken *)token cancel];
    }
}

static NSString *SWErrorLevelOf(NSError *error) {
    // A Swift error bridged to NSError is a __SwiftNativeNSError, whose -description describes the Swift value
    // (swift 8cc0db7, stdlib/public/runtime/ErrorObject.mm:70, 103-116).
    if ([error isKindOfClass:NSClassFromString(@"__SwiftNativeNSError")]) {
        return error.description;
    }
    NSMutableArray<NSString *> *details = [NSMutableArray array];
    NSString *description = error.userInfo[NSLocalizedDescriptionKey];
    if (description == nil && ![error.domain isEqualToString:NSOSStatusErrorDomain]) {
        description = error.localizedDescription;
    }
    for (NSString *detail in @[description ?: @"", error.userInfo[NSLocalizedFailureReasonErrorKey] ?: @"", error.userInfo[NSDebugDescriptionErrorKey] ?: @""]) {
        if (detail.length > 0 && ![details containsObject:detail]) {
            [details addObject:detail];
        }
    }
    NSString *identity = [NSString stringWithFormat:@"%@ %ld", error.domain, (long)error.code];
    return details.count == 0 ? identity : [NSString stringWithFormat:@"%@ (%@)", [details componentsJoinedByString:@": "], identity];
}

NSString *SWErrorMessageOf(NSError *error) {
    NSMutableArray<NSString *> *causes = [NSMutableArray array];
    for (NSError *cause in error.underlyingErrors) {
        [causes addObject:SWErrorMessageOf(cause)];
    }
    NSString *level = SWErrorLevelOf(error);
    return causes.count == 0 ? level : [NSString stringWithFormat:@"%@: %@", level, [causes componentsJoinedByString:@"; "]];
}

char *SWFailure(NSString *message) {
    return strdup(message.UTF8String);
}

char *SWFailureCause(NSString *operation, NSError *cause) {
    return SWFailure(cause == nil ? operation : [NSString stringWithFormat:@"%@: %@", operation, SWErrorMessageOf(cause)]);
}

char *SWFailureStatus(NSString *operation, OSStatus status) {
    return SWFailureCause(operation, [NSError errorWithDomain:NSOSStatusErrorDomain code:status userInfo:nil]);
}

void swipt_release(void *object) {
    CFRelease(object);
}
