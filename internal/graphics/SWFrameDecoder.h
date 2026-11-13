#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>

#include "graphics.h"

@interface SWFrameDecoder : NSObject
@property (nonatomic, copy) void (^onFrame)(CVPixelBufferRef image, double arrival, NSInteger turns);

- (BOOL)decode:(const swipt_graphics_unit *)unit;
@end
