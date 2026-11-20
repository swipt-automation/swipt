//go:build darwin && cgo

#import "SWCancelToken.h"

#include <stdatomic.h>

#import "SWError.h"

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

- (BOOL)failIfCancelled:(NSError **)error {
    if (!self.isCancelled) {
        return NO;
    }
    SWFailWith(error, SWErrorAborted());
    return YES;
}

@end
