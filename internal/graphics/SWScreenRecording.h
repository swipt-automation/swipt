#import <Foundation/Foundation.h>

#include "graphics_darwin.h"

#import "SWFrame.h"

@class SWCancelToken;
@class SWImageNode;
@class SWRaster;

@interface SWScreenRecording : NSObject
@property (nonatomic, readonly) NSString *info;
@property (nonatomic, readonly) double start;
@property (nonatomic, readonly) double duration;
@property (nonatomic, readonly) double scale;

- (instancetype)initWithRecording:(const swipt_graphics_recording *)recording;
- (void)pixelSizeAt:(double)time width:(NSInteger *)width height:(NSInteger *)height;
- (SWRaster *)rasterAt:(double)time token:(SWCancelToken *)token error:(NSError **)error;
- (SWImageNode *)nodeAt:(double)time;
- (BOOL)exportTo:(NSURL *)target mov:(BOOL)mov token:(SWCancelToken *)token error:(NSError **)error;
@end
