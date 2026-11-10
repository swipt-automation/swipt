#import <Foundation/Foundation.h>

@class SWCancelToken;
@class SWScreenStream;

@interface SWRecordingSession : NSObject
+ (instancetype)startInDirectory:(NSURL *)directory stream:(SWScreenStream *)stream token:(SWCancelToken *)token error:(NSError **)error;
- (BOOL)stop:(NSError **)error;
- (void)abandon;
@end
