#import <Foundation/Foundation.h>

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

@interface SWGoScreenStream : SWGoObject
@property (nonatomic, readonly) NSInteger width;
@property (nonatomic, readonly) NSInteger height;
@property (nonatomic, readonly) double scale;

+ (instancetype)openWithMode:(NSString *)mode maximumBitrate:(uint64_t)maximumBitrate error:(NSError **)error;
- (void)configureMode:(NSString *)mode maximumBitrate:(uint64_t)maximumBitrate;
- (void)requestKeyFrame;
- (void)close;
@end

void SWLogError(NSString *message);
void SWReportRecordingFailure(NSString *message);
