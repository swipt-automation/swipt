#import <Foundation/Foundation.h>

#include "graphics.h"

#import "SWFrame.h"

@class SWCancelToken;

@interface SWScreenFrames : NSObject
- (instancetype)initWithGeometry:(SWScreenGeometry)geometry;
- (BOOL)decode:(const swipt_graphics_unit *)unit;
- (void)endWithFailure:(NSError *)failure;
- (void)close;
- (SWFrame *)frameAfter:(NSInteger)previous until:(NSDate *)deadline token:(SWCancelToken *)token closed:(BOOL (^)(void))closed sequence:(NSInteger *)sequence error:(NSError **)error;
@end
