//go:build darwin && cgo

#import "SWCall.h"

#import "SWError.h"
#import "SWImageNode.h"

const NSInteger SWDefaultDifferenceThreshold = 24;

static const struct {
    const char *method;
    SWCallFunction function;
} SWCallTable[] = {
    {"screenshot", SWCallScreenshot},
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

@end
