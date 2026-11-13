//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <JavaScriptCore/JavaScriptCore.h>

#include "graphics.h"

#import "SWCall.h"
#import "SWCancelToken.h"
#import "SWEngine.h"
#import "SWError.h"
#import "SWGoBridge.h"
#import "SWJSON.h"
#import "SWNative.h"
#import "SWRecognition.h"
#import "SWScreenFrames.h"

enum {
    SWCommandInstall = 0,
    SWCommandUninstall = 1,
};

static NSMutableDictionary<NSNumber *, SWEngine *> *SWEngines(void) {
    static NSMutableDictionary<NSNumber *, SWEngine *> *engines;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        engines = [NSMutableDictionary dictionary];
    });
    return engines;
}

static SWEngine *SWEngineFor(uint64_t identifier) {
    NSMutableDictionary<NSNumber *, SWEngine *> *engines = SWEngines();
    @synchronized(engines) {
        return engines[@(identifier)];
    }
}

static dispatch_queue_t SWWorkQueue(void) {
    static dispatch_queue_t queue;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        queue = dispatch_queue_create("io.nekohasekai.swipt.graphics", dispatch_queue_attr_make_with_qos_class(DISPATCH_QUEUE_CONCURRENT, QOS_CLASS_USER_INITIATED, 0));
    });
    return queue;
}

void swipt_graphics_native(void *context, int32_t command, void *value, uint64_t identifier) {
    @autoreleasepool {
        NSMutableDictionary<NSNumber *, SWEngine *> *engines = SWEngines();
        if (command == SWCommandInstall) {
            JSContext *wrapped = [JSContext contextWithJSGlobalContextRef:context];
            JSValue *target = [JSValue valueWithJSValueRef:value inContext:wrapped];
            if (wrapped == nil || target == nil) {
                return;
            }
            SWEngine *engine = [[SWEngine alloc] initWithContext:wrapped];
            @synchronized(engines) {
                engines[@(identifier)] = engine;
            }
            [target setValue:[[SWNative alloc] initWithEngine:engine] forProperty:@"graphics"];
        } else if (command == SWCommandUninstall) {
            SWEngine *engine;
            @synchronized(engines) {
                engine = engines[@(identifier)];
                [engines removeObjectForKey:@(identifier)];
            }
            [engine close];
        }
    }
}

void swipt_graphics_warm(void) {
    SWRecognitionWarm();
}

static SWCall *SWPrepareCall(uint64_t engineIdentifier, int64_t callIdentifier, SWCallContext *context) {
    SWEngine *engine = SWEngineFor(engineIdentifier);
    SWCall *call = [engine takePrepared:(NSInteger)callIdentifier];
    if (call == nil) {
        return nil;
    }
    call.engine = engine;
    call.context = context;
    call.token = [[SWCancelToken alloc] initWithCancelled:^BOOL {
        return context.cancelled;
    }];
    return call;
}

char *swipt_graphics_run(uint64_t engine, int64_t identifier, uintptr_t scope) {
    @autoreleasepool {
        SWCallContext *context = [[SWCallContext alloc] initWithHandle:scope];
        SWCall *call = SWPrepareCall(engine, identifier, context);
        if (call == nil) {
            return strdup(SWOutcomeFailure(SWErrorEnded()).UTF8String);
        }
        __block NSString *outcome = nil;
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        dispatch_async(SWWorkQueue(), ^{
            @autoreleasepool {
                NSError *failure = nil;
                id value = SWCallExecute(call, &failure);
                outcome = value != nil ? SWOutcomeSuccess(value) : SWOutcomeFailure(failure);
            }
            dispatch_semaphore_signal(done);
        });
        while (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 50 * NSEC_PER_MSEC)) != 0) {
            if (context.cancelled) {
                [call.token cancel];
                return strdup(SWOutcomeFailure(SWErrorAborted()).UTF8String);
            }
        }
        return strdup(outcome.UTF8String);
    }
}

char *swipt_graphics_watch(uint64_t engine, int64_t identifier, uintptr_t scope) {
    @autoreleasepool {
        SWCallContext *context = [[SWCallContext alloc] initWithHandle:scope];
        SWCall *call = SWPrepareCall(engine, identifier, context);
        if (call == nil) {
            return strdup(SWOutcomeFailure(SWErrorEnded()).UTF8String);
        }
        NSError *failure = nil;
        BOOL watched = SWCallScreenFrames(call, &failure);
        return strdup((watched ? SWOutcomeSuccess(NSNull.null) : SWOutcomeFailure(failure)).UTF8String);
    }
}

void *swipt_graphics_frames_create(int64_t width, int64_t height, double scale) {
    @autoreleasepool {
        SWScreenGeometry geometry = {.width = (NSInteger)width, .height = (NSInteger)height, .scale = scale};
        return (__bridge_retained void *)[[SWScreenFrames alloc] initWithGeometry:geometry];
    }
}

void *swipt_graphics_frames_retain(void *frames) {
    return (void *)CFRetain(frames);
}

bool swipt_graphics_frames_decode(void *frames, swipt_graphics_unit unit) {
    @autoreleasepool {
        return [(__bridge SWScreenFrames *)frames decode:&unit];
    }
}

void swipt_graphics_frames_end(void *frames, const char *failure) {
    @autoreleasepool {
        [(__bridge SWScreenFrames *)frames endWithFailure:SWErrorFromFailure([NSString stringWithUTF8String:failure])];
    }
}

void swipt_graphics_frames_close(void *frames) {
    @autoreleasepool {
        SWScreenFrames *closing = (__bridge_transfer SWScreenFrames *)frames;
        [closing close];
    }
}
