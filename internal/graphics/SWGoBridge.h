#import <Foundation/Foundation.h>

@class SWCancelToken;
@class SWFrame;
@class SWScreenFrames;

@interface SWGoObject : NSObject
@property (nonatomic, readonly) uintptr_t handle;

- (instancetype)initWithHandle:(uintptr_t)handle;
@end

@interface SWGoFile : SWGoObject
@property (nonatomic, readonly) NSURL *url;

- (BOOL)commit:(NSError **)error;
- (void)discard;
@end

@interface SWCallContext : SWGoObject
@property (nonatomic, readonly) BOOL cancelled;

- (NSData *)readRoot:(NSString *)root path:(NSString *)path error:(NSError **)error;
- (BOOL)writeData:(NSData *)data root:(NSString *)root path:(NSString *)path error:(NSError **)error;
- (NSURL *)directoryAtRoot:(NSString *)root path:(NSString *)path create:(BOOL)create error:(NSError **)error;
- (SWGoFile *)stageFileAtRoot:(NSString *)root path:(NSString *)path error:(NSError **)error;
- (NSData *)screenshotWithScale:(double *)scale error:(NSError **)error;
- (void)publish:(NSString *)value;
@end

@interface SWGoResource : SWGoObject
- (void)relinquish;
@end

@interface SWRecordingSession : SWGoResource
- (BOOL)stop:(NSError **)error;
@end

@interface SWScreenOwner : SWGoObject
- (void)close;
@end

@interface SWScreenStream : SWGoResource
@property (nonatomic, readonly) BOOL closed;

+ (instancetype)openWithContext:(SWCallContext *)context owner:(SWScreenOwner *)owner mode:(NSString *)mode maximumBitrate:(uint64_t)maximumBitrate error:(NSError **)error;
- (SWFrame *)latestFrame:(SWCancelToken *)token error:(NSError **)error;
- (SWFrame *)frameAfter:(NSInteger)previous until:(NSDate *)deadline token:(SWCancelToken *)token sequence:(NSInteger *)sequence error:(NSError **)error;
- (SWRecordingSession *)recordWithContext:(SWCallContext *)context root:(NSString *)root path:(NSString *)path error:(NSError **)error;
- (void)close;
@end

void SWLogError(NSString *message);
