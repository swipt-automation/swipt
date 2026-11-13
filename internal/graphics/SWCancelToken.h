#import <Foundation/Foundation.h>

@interface SWCancelToken : NSObject
@property (nonatomic, readonly) BOOL isCancelled;

- (instancetype)initWithCancelled:(BOOL (^)(void))cancelled;
- (BOOL)failIfCancelled:(NSError **)error;
@end
