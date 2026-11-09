#import <Foundation/Foundation.h>

#import "SWFrame.h"

@class SWCancelToken;
@class SWImageNode;
@class SWRaster;

extern NSString *const SWRecordingManifest;

@interface SWScreenRecording : NSObject
@property (nonatomic, readonly) double start;
@property (nonatomic, readonly) double duration;
@property (nonatomic, readonly) double scale;

+ (instancetype)openDirectory:(NSURL *)directory error:(NSError **)error;
- (void)pixelSizeAt:(double)time width:(NSInteger *)width height:(NSInteger *)height;
- (SWRaster *)rasterAt:(double)time token:(SWCancelToken *)token error:(NSError **)error;
- (SWImageNode *)nodeAt:(double)time;
- (BOOL)exportTo:(NSURL *)target mov:(BOOL)mov token:(SWCancelToken *)token error:(NSError **)error;
- (NSString *)info;
@end
