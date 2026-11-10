#import <Foundation/Foundation.h>

#import "SWFrame.h"

@class SWAccessUnit;
@class SWCancelToken;
@class SWRecordingSession;

extern const NSTimeInterval SWScreenFirstFrameLimit;
extern NSString *const SWScreenModeRemoteDesktop;
extern NSString *const SWScreenModeCoreDevice;

@interface SWScreenStream : NSObject
@property (nonatomic, readonly, copy) NSString *mode;
@property (nonatomic, readonly) uint64_t maximumBitrate;
@property (nonatomic, readonly) SWScreenGeometry geometry;
@property (atomic, readonly) BOOL closed;

+ (instancetype)openWithMode:(NSString *)mode maximumBitrate:(uint64_t)maximumBitrate error:(NSError **)error;
- (SWFrame *)latestFrame:(SWCancelToken *)token error:(NSError **)error;
- (SWFrame *)frameAfter:(NSInteger)previous token:(SWCancelToken *)token sequence:(NSInteger *)sequence error:(NSError **)error;
- (void)requestKeyFrame;
- (NSInteger)addUnitListener:(void (^)(SWAccessUnit *unit))listener;
- (void)removeListener:(NSInteger)identifier;
- (BOOL)addRecording:(SWRecordingSession *)recording;
- (void)removeRecording:(SWRecordingSession *)recording;
- (void)close;
@end
