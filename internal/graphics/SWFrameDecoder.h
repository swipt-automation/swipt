#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>

#include "graphics_darwin.h"

@interface SWFrameDecoder : NSObject
- (instancetype)initWithFrame:(void (^)(CVPixelBufferRef image, double arrival, NSInteger turns))onFrame failure:(void (^)(NSString *message))onFailure;
- (BOOL)decode:(const swipt_graphics_unit *)unit;
- (void)close;
@end
