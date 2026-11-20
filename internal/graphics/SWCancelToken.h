#import <Foundation/Foundation.h>

@interface SWCancelToken : NSObject
@property (nonatomic, readonly) BOOL isCancelled;

- (void)cancel;
- (void (^)(void))whenCancelled:(void (^)(void))handler;
- (BOOL)failIfCancelled:(NSError **)error;
@end
