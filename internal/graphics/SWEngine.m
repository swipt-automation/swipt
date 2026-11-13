//go:build darwin && cgo

#import "SWEngine.h"

#import <JavaScriptCore/JavaScriptCore.h>

#import "SWGoBridge.h"

@implementation SWEngine {
    JSContext *_context;
    NSLock *_lock;
    NSInteger _next;
    NSMutableDictionary<NSNumber *, SWCall *> *_prepared;
    NSMutableDictionary<NSNumber *, id> *_objects;
    BOOL _closed;
}

- (instancetype)initWithContext:(JSContext *)context {
    self = [super init];
    if (self) {
        _context = context;
        _lock = [NSLock new];
        _prepared = [NSMutableDictionary dictionary];
        _objects = [NSMutableDictionary dictionary];
        _screens = [SWScreenOwner new];
    }
    return self;
}

- (NSInteger)prepare:(SWCall *)call {
    [_lock lock];
    _next += 1;
    NSInteger identifier = _next;
    _prepared[@(identifier)] = call;
    [_lock unlock];
    return identifier;
}

- (SWCall *)takePrepared:(NSInteger)identifier {
    [_lock lock];
    SWCall *call = _prepared[@(identifier)];
    [_prepared removeObjectForKey:@(identifier)];
    [_lock unlock];
    return call;
}

- (NSInteger)registerObject:(id)object {
    [_lock lock];
    _next += 1;
    NSInteger identifier = _next;
    BOOL accepted = !_closed;
    if (accepted) {
        _objects[@(identifier)] = object;
    }
    [_lock unlock];
    if (!accepted && [object isKindOfClass:SWGoResource.class]) {
        [(SWGoResource *)object relinquish];
    }
    return identifier;
}

- (id)take:(NSInteger)identifier {
    [_lock lock];
    id object = _objects[@(identifier)];
    [_objects removeObjectForKey:@(identifier)];
    [_lock unlock];
    return object;
}

- (void)drop:(NSInteger)identifier {
    [self take:identifier];
}

- (void)close {
    [_lock lock];
    _closed = YES;
    [_prepared removeAllObjects];
    NSArray *objects = _objects.allValues;
    [_objects removeAllObjects];
    _context = nil;
    [_lock unlock];
    for (id object in objects) {
        if ([object isKindOfClass:SWGoResource.class]) {
            [(SWGoResource *)object relinquish];
        }
    }
    [_screens close];
}

@end
