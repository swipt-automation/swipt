//go:build darwin && cgo

#import "SWCall.h"

#import "SWError.h"
#import "SWImageNode.h"
#import "SWScreenRecording.h"

const NSInteger SWDefaultDifferenceThreshold = 24;

static const struct {
    const char *method;
    SWCallFunction function;
} SWCallTable[] = {
    {"screenshot", SWCallScreenshot},
    {"screen.open", SWCallScreenOpen},
    {"screen.close", SWCallScreenClose},
    {"screen.frame", SWCallScreenFrame},
    {"screen.record", SWCallScreenRecord},
    {"decode", SWCallDecode},
    {"encode", SWCallEncode},
    {"save", SWCallSave},
    {"pixel", SWCallPixel},
    {"stats", SWCallStats},
    {"find", SWCallFind},
    {"colors", SWCallColors},
    {"components", SWCallComponents},
    {"text", SWCallText},
    {"diff", SWCallDiff},
    {"distance", SWCallDistance},
    {"nearest", SWCallNearest},
    {"barcodes", SWCallBarcodes},
    {"recording.stop", SWCallRecordingStop},
    {"recording.open", SWCallRecordingOpen},
    {"recording.export", SWCallRecordingExport},
};

id SWCallExecute(SWCall *call, NSError **error) {
    const char *method = call.method.UTF8String;
    for (size_t index = 0; index < sizeof(SWCallTable) / sizeof(SWCallTable[0]); index++) {
        if (strcmp(SWCallTable[index].method, method) == 0) {
            return SWCallTable[index].function(call, error);
        }
    }
    SWFail(error, SWErrorNameType, @"graphics: there is no call %@", call.method);
    return nil;
}

@implementation SWCall

- (instancetype)initWithMethod:(NSString *)method arguments:(SWArguments *)arguments objects:(NSArray *)objects {
    self = [super init];
    if (self) {
        _method = method;
        _arguments = arguments;
        _objects = objects;
    }
    return self;
}

- (SWImageNode *)image:(NSUInteger)index error:(NSError **)error {
    id object = index < _objects.count ? _objects[index] : nil;
    if (![object isKindOfClass:SWImageNode.class]) {
        SWFail(error, SWErrorNameType, @"graphics: argument %lu of %@ is not an Image", (unsigned long)index + 1, _method);
        return nil;
    }
    return object;
}

- (SWScreenRecording *)recording:(NSError **)error {
    id object = _objects.firstObject;
    if (![object isKindOfClass:SWScreenRecording.class]) {
        SWFail(error, SWErrorNameType, @"graphics: %@ needs a ScreenRecording", _method);
        return nil;
    }
    return object;
}

@end
