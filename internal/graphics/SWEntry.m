//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <JavaScriptCore/JavaScriptCore.h>

#include "_cgo_export.h"
#include "graphics_darwin.h"

#import "SWArguments.h"
#import "SWCall.h"
#import "SWCancelToken.h"
#import "SWError.h"
#import "SWFrame.h"
#import "SWFrameDecoder.h"
#import "SWGoBridge.h"
#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWNative.h"
#import "SWRecognition.h"
#import "SWScreenRecording.h"

void swipt_graphics_install(void *context, int32_t command, void *value, uint64_t engine) {
    @autoreleasepool {
        JSContext *wrapped = [JSContext contextWithJSGlobalContextRef:context];
        JSValue *target = [JSValue valueWithJSValueRef:value inContext:wrapped];
        [target setValue:[[SWNative alloc] initWithEngine:(uintptr_t)engine] forProperty:@"graphics"];
    }
}

void swipt_graphics_warm(uintptr_t logger) {
    SWRecognitionWarm(^(NSError *failure) {
        if (failure != nil) {
            swiptGraphicsLog(logger, (char *)[NSString stringWithFormat:@"warm the text recognizer: %@", SWErrorMessageOf(failure)].UTF8String);
        }
        swiptGraphicsDeleteHandle(logger);
    });
}

static SWCancelToken *SWTokenOf(SWCallContext *context) {
    return [[SWCancelToken alloc] initWithCancelled:^BOOL {
        return context.cancelled;
    }];
}

char *swipt_graphics_execute(const char *method, const char *arguments, void *const *objects, size_t count, uintptr_t scope) {
    @autoreleasepool {
        NSError *failure = nil;
        SWArguments *parsed = [[SWArguments alloc] initWithJSON:[NSString stringWithUTF8String:arguments] error:&failure];
        if (parsed == nil) {
            return strdup(SWOutcomeFailure(failure).UTF8String);
        }
        NSMutableArray *unwrapped = [NSMutableArray arrayWithCapacity:count];
        for (size_t index = 0; index < count; index++) {
            [unwrapped addObject:objects[index] != NULL ? (__bridge id)objects[index] : NSNull.null];
        }
        SWCall *call = [[SWCall alloc] initWithMethod:[NSString stringWithUTF8String:method] arguments:parsed objects:unwrapped];
        call.context = [[SWCallContext alloc] initWithScope:scope];
        call.token = SWTokenOf(call.context);
        id value = SWCallExecute(call, &failure);
        return strdup((value != nil ? SWOutcomeSuccess(value) : SWOutcomeFailure(failure)).UTF8String);
    }
}

void *swipt_graphics_decoder_create(uintptr_t stream, int64_t width, int64_t height, double scale) {
    @autoreleasepool {
        SWScreenGeometry geometry = {.width = (NSInteger)width, .height = (NSInteger)height, .scale = scale};
        SWFrameDecoder *decoder = [[SWFrameDecoder alloc] initWithFrame:^(CVPixelBufferRef image, double arrival, NSInteger turns) {
            SWFrame *frame = [[SWFrame alloc] initWithPixelBuffer:image time:arrival turns:turns geometry:geometry];
            swiptGraphicsScreenFrame(stream, (void *)CFBridgingRetain([SWImageNode nodeWithFrame:frame]));
        } failure:^(NSString *message) {
            swiptGraphicsScreenLog(stream, (char *)message.UTF8String);
        }];
        return (void *)CFBridgingRetain(decoder);
    }
}

bool swipt_graphics_decoder_decode(void *decoder, swipt_graphics_unit unit) {
    @autoreleasepool {
        return [(__bridge SWFrameDecoder *)decoder decode:&unit];
    }
}

void swipt_graphics_decoder_close(void *decoder) {
    @autoreleasepool {
        SWFrameDecoder *closing = CFBridgingRelease(decoder);
        [closing close];
    }
}

void *swipt_graphics_recording_open(swipt_graphics_recording recording) {
    @autoreleasepool {
        return (void *)CFBridgingRetain([[SWScreenRecording alloc] initWithRecording:&recording]);
    }
}

char *swipt_graphics_recording_export(void *recording, const char *target, bool mp4, uintptr_t scope) {
    @autoreleasepool {
        SWCallContext *context = [[SWCallContext alloc] initWithScope:scope];
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:target] isDirectory:NO];
        NSError *failure = nil;
        BOOL exported = [(__bridge SWScreenRecording *)recording exportTo:url mov:!mp4 token:SWTokenOf(context) error:&failure];
        return strdup((exported ? SWOutcomeSuccess(NSNull.null) : SWOutcomeFailure(failure)).UTF8String);
    }
}
