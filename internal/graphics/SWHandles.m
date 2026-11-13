//go:build darwin && cgo

#import "SWHandles.h"

#include "_cgo_export.h"

#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWJavaScript.h"
#import "SWRaster.h"
#import "SWRecognition.h"
#import "SWScreenRecording.h"

static uint8_t SWClampChannel(NSInteger value) {
    return (uint8_t)MIN(MAX(value, 0), 255);
}

static SWImageHandle *SWReported(SWImageNode *node) {
    SWJavaScriptReportCost(node.estimatedBytes);
    return [[SWImageHandle alloc] initWithNode:node];
}

@implementation SWImageHandle

- (instancetype)initWithNode:(SWImageNode *)node {
    self = [super init];
    if (self) {
        _node = node;
    }
    return self;
}

- (double)width {
    return SWRounded(CGRectGetWidth(_node.frame));
}

- (double)height {
    return SWRounded(CGRectGetHeight(_node.frame));
}

- (double)scale {
    return SWRounded(_node.scale);
}

- (NSArray<NSNumber *> *)frame {
    return SWRectJSON(_node.frame);
}

- (double)time {
    return _node.time;
}

- (double)timestamp {
    return _node.timestamp;
}

- (NSInteger)pixelWidth {
    return _node.pixelWidth;
}

- (NSInteger)pixelHeight {
    return _node.pixelHeight;
}

- (JSValue *)crop:(double)x :(double)y :(double)width :(double)height {
    NSError *error = nil;
    SWImageNode *cropped = [_node cropInSpace:CGRectMake(x, y, width, height) error:&error];
    if (cropped == nil) {
        return SWJavaScriptFail(error);
    }
    return [JSValue valueWithObject:[[SWImageHandle alloc] initWithNode:cropped] inContext:JSContext.currentContext];
}

- (SWImageHandle *)resize:(NSInteger)width :(NSInteger)height {
    return SWReported([_node resizeToWidth:width height:height]);
}

- (SWImageHandle *)rotate:(double)degrees {
    return SWReported([_node rotateDegrees:degrees]);
}

- (SWImageHandle *)grayscale {
    return SWReported(_node.grayscaleNode);
}

- (SWImageHandle *)threshold:(NSInteger)level {
    return SWReported([_node thresholdNode:level]);
}

- (SWImageHandle *)mask:(NSInteger)lowRed :(NSInteger)lowGreen :(NSInteger)lowBlue :(NSInteger)highRed :(NSInteger)highGreen :(NSInteger)highBlue {
    const uint8_t low[3] = {SWClampChannel(lowRed), SWClampChannel(lowGreen), SWClampChannel(lowBlue)};
    const uint8_t high[3] = {SWClampChannel(highRed), SWClampChannel(highGreen), SWClampChannel(highBlue)};
    return SWReported([_node maskNodeLow:low high:high]);
}

- (SWImageHandle *)invert {
    return SWReported(_node.invertNode);
}

- (SWImageHandle *)dilate:(NSInteger)radius {
    return SWReported([_node morphNode:radius dilate:YES]);
}

- (SWImageHandle *)erode:(NSInteger)radius {
    return SWReported([_node morphNode:radius dilate:NO]);
}

- (JSValue *)showBytes {
    NSError *error = nil;
    SWRaster *raster = [_node raster:nil error:&error];
    NSData *data = raster == nil ? nil : SWRasterEncode(raster, @"png", nil, _node.scale, &error);
    return data == nil ? SWJavaScriptFail(error) : SWJavaScriptBytes(data);
}

@end

@implementation SWResourceHandle

- (instancetype)initWithHandle:(uintptr_t)handle {
    self = [super init];
    if (self) {
        _handle = handle;
    }
    return self;
}

- (void)dealloc {
    swiptGraphicsRelease(_handle);
}

@end

@implementation SWScreenRecordingHandle {
    SWScreenRecording *_recording;
}

- (instancetype)initWithHandle:(uintptr_t)handle recording:(SWScreenRecording *)recording {
    self = [super initWithHandle:handle];
    if (self) {
        _recording = recording;
    }
    return self;
}

- (NSString *)info {
    return _recording.info;
}

- (SWImageHandle *)frame:(double)time {
    return SWReported([_recording nodeAt:time]);
}

@end

@implementation SWTextHandle {
    SWRecognizedText *_text;
}

- (instancetype)initWithText:(SWRecognizedText *)text {
    self = [super init];
    if (self) {
        _text = text;
    }
    return self;
}

- (JSValue *)boundingBox:(NSInteger)line :(NSInteger)candidate :(NSInteger)start :(NSInteger)end {
    JSContext *context = JSContext.currentContext;
    NSError *error = nil;
    NSDictionary *box = [_text boundingBoxLine:line candidate:candidate start:start end:end error:&error];
    if (error != nil) {
        return SWJavaScriptFail(error);
    }
    return box == nil ? [JSValue valueWithNullInContext:context] : [JSValue valueWithObject:box inContext:context];
}

@end
