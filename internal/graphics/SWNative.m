//go:build darwin && cgo

#import "SWNative.h"

#include "_cgo_export.h"

#import "SWError.h"
#import "SWHandles.h"
#import "SWImageNode.h"
#import "SWJavaScript.h"
#import "SWRecognition.h"
#import "SWScreenRecording.h"

@implementation SWNative {
    uintptr_t _engine;
}

- (instancetype)initWithEngine:(uintptr_t)engine {
    self = [super init];
    if (self) {
        _engine = engine;
    }
    return self;
}

- (void)dealloc {
    swiptGraphicsDeleteHandle(_engine);
}

static swipt_graphics_object SWObjectOf(JSValue *item) {
    swipt_graphics_object object = {0};
    NSData *data = SWJavaScriptData(item);
    id unwrapped = data ?: item.toObject;
    if ([unwrapped isKindOfClass:SWImageHandle.class]) {
        unwrapped = ((SWImageHandle *)unwrapped).node;
    }
    if ([unwrapped isKindOfClass:SWResourceHandle.class]) {
        object.kind = SWIPT_GRAPHICS_HANDLE;
        object.handle = ((SWResourceHandle *)unwrapped).handle;
    } else if ([unwrapped isKindOfClass:NSData.class] || [unwrapped isKindOfClass:SWImageNode.class]) {
        object.kind = SWIPT_GRAPHICS_NATIVE;
        object.native = (void *)CFBridgingRetain(unwrapped);
    }
    return object;
}

- (JSValue *)prepare:(NSString *)method :(NSString *)arguments :(JSValue *)objects {
    JSContext *context = JSContext.currentContext;
    int32_t count = MAX([objects valueForProperty:@"length"].toInt32, 0);
    swipt_graphics_object *items = calloc(MAX(count, 1), sizeof(swipt_graphics_object));
    for (int32_t index = 0; index < count; index++) {
        items[index] = SWObjectOf([objects valueAtIndex:index]);
    }
    swipt_graphics_failure failure = {0};
    int64_t identifier = swiptGraphicsPrepare(_engine, (char *)method.UTF8String, (char *)arguments.UTF8String, items, count, &failure);
    free(items);
    if (failure.name != NULL) {
        NSError *error = SWMakeError([NSString stringWithUTF8String:failure.name], [NSString stringWithUTF8String:failure.message]);
        free(failure.name);
        free(failure.message);
        return SWJavaScriptFail(error);
    }
    return [JSValue valueWithDouble:(double)identifier inContext:context];
}

- (JSValue *)take:(NSInteger)identifier {
    JSContext *context = JSContext.currentContext;
    swipt_graphics_taken taken = swiptGraphicsTake(_engine, identifier);
    switch (taken.kind) {
    case SWIPT_GRAPHICS_HANDLE:
        return [JSValue valueWithObject:[[SWResourceHandle alloc] initWithHandle:taken.handle] inContext:context];
    case SWIPT_GRAPHICS_RECORDING:
        return [JSValue valueWithObject:[[SWScreenRecordingHandle alloc] initWithHandle:taken.handle recording:CFBridgingRelease(taken.native)] inContext:context];
    case SWIPT_GRAPHICS_NATIVE:
        break;
    default:
        return [JSValue valueWithNullInContext:context];
    }
    id object = CFBridgingRelease(taken.native);
    if ([object isKindOfClass:SWImageNode.class]) {
        SWJavaScriptReportCost(((SWImageNode *)object).estimatedBytes);
        return [JSValue valueWithObject:[[SWImageHandle alloc] initWithNode:object] inContext:context];
    }
    if ([object isKindOfClass:SWRecognizedText.class]) {
        return [JSValue valueWithObject:[[SWTextHandle alloc] initWithText:object] inContext:context];
    }
    return [JSValue valueWithNullInContext:context];
}

- (JSValue *)bytes:(NSInteger)identifier {
    swipt_graphics_taken taken = swiptGraphicsTake(_engine, identifier);
    if (taken.kind == SWIPT_GRAPHICS_HANDLE || taken.kind == SWIPT_GRAPHICS_RECORDING) {
        swiptGraphicsRelease(taken.handle);
    }
    id data = taken.native != NULL ? CFBridgingRelease(taken.native) : nil;
    if (![data isKindOfClass:NSData.class]) {
        return [JSValue valueWithNullInContext:JSContext.currentContext];
    }
    return SWJavaScriptBytes(data);
}

@end
