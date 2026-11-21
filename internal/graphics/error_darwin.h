#import <Foundation/Foundation.h>

@interface SWCancelToken : NSObject
@property (nonatomic, readonly) BOOL isCancelled;

- (void)cancel;
- (void (^)(void))whenCancelled:(void (^)(void))handler;
@end

NSString *SWErrorMessageOf(NSError *error);
char *SWFailure(NSString *message);
char *SWFailureCause(NSString *operation, NSError *cause);
char *SWFailureStatus(NSString *operation, OSStatus status);
