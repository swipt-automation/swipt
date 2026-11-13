//go:build darwin && cgo

#import "SWScreenFrames.h"

#import "SWCancelToken.h"
#import "SWError.h"
#import "SWFrameDecoder.h"

@implementation SWScreenFrames {
    NSCondition *_state;
    SWScreenGeometry _geometry;
    SWFrameDecoder *_decoder;
    SWFrame *_latest;
    NSInteger _sequence;
    NSError *_failure;
    BOOL _closed;
}

- (instancetype)initWithGeometry:(SWScreenGeometry)geometry {
    self = [super init];
    if (self) {
        _state = [NSCondition new];
        _geometry = geometry;
        _decoder = [SWFrameDecoder new];
        __weak SWScreenFrames *weakSelf = self;
        _decoder.onFrame = ^(CVPixelBufferRef image, double arrival, NSInteger turns) {
            [weakSelf decoded:image arrival:arrival turns:turns];
        };
    }
    return self;
}

- (BOOL)decode:(const swipt_graphics_unit *)unit {
    return [_decoder decode:unit];
}

- (void)decoded:(CVPixelBufferRef)image arrival:(double)arrival turns:(NSInteger)turns {
    [_state lock];
    if (!_closed && _failure == nil) {
        _latest = [[SWFrame alloc] initWithPixelBuffer:image time:arrival turns:turns geometry:_geometry];
        _sequence += 1;
        [_state broadcast];
    }
    [_state unlock];
}

- (void)endWithFailure:(NSError *)failure {
    [_state lock];
    _failure = failure;
    _latest = nil;
    [_state broadcast];
    [_state unlock];
}

- (void)close {
    [_state lock];
    _closed = YES;
    _latest = nil;
    [_state broadcast];
    [_state unlock];
}

- (SWFrame *)frameAfter:(NSInteger)previous until:(NSDate *)deadline token:(SWCancelToken *)token closed:(BOOL (^)(void))closed sequence:(NSInteger *)sequence error:(NSError **)error {
    [_state lock];
    while (!_closed && !closed()) {
        if (_failure != nil) {
            SWFailWith(error, _failure);
            break;
        }
        if (_latest != nil && _sequence > previous) {
            SWFrame *latest = _latest;
            *sequence = _sequence;
            [_state unlock];
            return latest;
        }
        if (token.isCancelled) {
            SWFailWith(error, SWErrorAborted());
            break;
        }
        NSDate *now = NSDate.date;
        if ([now compare:deadline] != NSOrderedAscending) {
            break;
        }
        [_state waitUntilDate:[deadline earlierDate:[now dateByAddingTimeInterval:0.05]]];
    }
    [_state unlock];
    return nil;
}

@end
