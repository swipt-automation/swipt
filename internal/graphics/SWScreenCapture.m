//go:build darwin && cgo

#import "SWScreenCapture.h"

#import "SWAccessUnit.h"
#import "SWCancelToken.h"
#import "SWDepacketizer.h"
#import "SWError.h"
#import "SWFrameDecoder.h"
#import "SWGoBridge.h"
#import "SWScreenStream.h"

static const NSInteger SWPacketHeaderSize = 13;

@implementation SWScreenCapture {
    dispatch_queue_t _queue;
    NSCondition *_state;
    SWGoScreenStream *_source;
    SWScreenGeometry _geometry;
    NSMutableArray<SWScreenStream *> *_streams;
    BOOL _opening;
    NSString *_mode;
    uint64_t _maximumBitrate;
    SWFrame *_latest;
    NSInteger _sequence;
    NSError *_failure;
    NSMutableDictionary<NSNumber *, void (^)(SWAccessUnit *)> *_unitListeners;
    NSInteger _nextListener;
    SWDepacketizer *_depacketizer;
    SWFrameDecoder *_decoder;
}

+ (SWScreenCapture *)shared {
    static SWScreenCapture *capture;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        capture = [SWScreenCapture new];
    });
    return capture;
}

- (instancetype)init {
    self = [super init];
    if (self) {
        _queue = dispatch_queue_create("io.nekohasekai.swipt.graphics.screen", dispatch_queue_attr_make_with_qos_class(DISPATCH_QUEUE_SERIAL, QOS_CLASS_USER_INTERACTIVE, 0));
        _state = [NSCondition new];
        _streams = [NSMutableArray array];
        _unitListeners = [NSMutableDictionary dictionary];
        _depacketizer = [SWDepacketizer new];
        _decoder = [SWFrameDecoder new];
        __weak SWScreenCapture *weakSelf = self;
        _decoder.onFrame = ^(CVPixelBufferRef image, SWAccessUnit *unit) {
            [weakSelf decoded:image unit:unit];
        };
    }
    return self;
}

- (void)mergedMode:(NSString **)mode maximumBitrate:(uint64_t *)maximumBitrate {
    *mode = SWScreenModeCoreDevice;
    *maximumBitrate = 0;
    for (SWScreenStream *stream in _streams) {
        if (![stream.mode isEqualToString:SWScreenModeCoreDevice]) {
            *mode = SWScreenModeRemoteDesktop;
            *maximumBitrate = MAX(*maximumBitrate, stream.maximumBitrate);
        }
    }
}

- (BOOL)addStream:(SWScreenStream *)stream geometry:(SWScreenGeometry *)geometry error:(NSError **)error {
    [_state lock];
    [_streams addObject:stream];
    while (_opening) {
        [_state wait];
    }
    NSString *mode = nil;
    uint64_t maximumBitrate = 0;
    [self mergedMode:&mode maximumBitrate:&maximumBitrate];
    if (_source != nil) {
        SWGoScreenStream *source = _source;
        *geometry = _geometry;
        BOOL changed = ![mode isEqualToString:_mode] || maximumBitrate != _maximumBitrate;
        _mode = mode;
        _maximumBitrate = maximumBitrate;
        if (changed) {
            dispatch_async(_queue, ^{
                [source configureMode:mode maximumBitrate:maximumBitrate];
            });
        }
        [_state unlock];
        return YES;
    }
    _opening = YES;
    dispatch_async(_queue, ^{
        [self->_depacketizer reset];
        [self->_decoder invalidate];
    });
    [_state unlock];
    NSError *failure = nil;
    SWGoScreenStream *opened = [SWGoScreenStream openWithMode:mode maximumBitrate:maximumBitrate error:&failure];
    [_state lock];
    _opening = NO;
    [_state broadcast];
    if (opened == nil) {
        [_streams removeObjectIdenticalTo:stream];
        [_state unlock];
        SWFailWith(error, failure);
        return NO;
    }
    _geometry = (SWScreenGeometry){.width = opened.width, .height = opened.height, .scale = opened.scale};
    *geometry = _geometry;
    _source = opened;
    _mode = mode;
    _maximumBitrate = maximumBitrate;
    _failure = nil;
    _latest = nil;
    NSString *merged = nil;
    uint64_t mergedBitrate = 0;
    [self mergedMode:&merged maximumBitrate:&mergedBitrate];
    BOOL changed = ![merged isEqualToString:_mode] || mergedBitrate != _maximumBitrate;
    _mode = merged;
    _maximumBitrate = mergedBitrate;
    if (changed) {
        dispatch_async(_queue, ^{
            [opened configureMode:merged maximumBitrate:mergedBitrate];
        });
    }
    [_state unlock];
    return YES;
}

- (void)removeStream:(SWScreenStream *)stream {
    [_state lock];
    [_streams removeObjectIdenticalTo:stream];
    [_state broadcast];
    SWGoScreenStream *closing = nil;
    NSString *mode = nil;
    uint64_t maximumBitrate = 0;
    if (_streams.count == 0) {
        closing = _source;
        _source = nil;
        _latest = nil;
    } else if (_source != nil) {
        [self mergedMode:&mode maximumBitrate:&maximumBitrate];
        if (![mode isEqualToString:_mode] || maximumBitrate != _maximumBitrate) {
            SWGoScreenStream *configuring = _source;
            _mode = mode;
            _maximumBitrate = maximumBitrate;
            dispatch_async(_queue, ^{
                [configuring configureMode:mode maximumBitrate:maximumBitrate];
            });
        }
    }
    [_state unlock];
    [closing close];
}

- (void)requestKeyFrame {
    [_state lock];
    SWGoScreenStream *source = _source;
    [_state unlock];
    [source requestKeyFrame];
}

- (SWFrame *)frameAfter:(NSInteger)previous stream:(SWScreenStream *)stream until:(NSDate *)deadline token:(SWCancelToken *)token sequence:(NSInteger *)sequence error:(NSError **)error {
    [_state lock];
    while (!stream.closed) {
        if (_failure != nil) {
            SWFailWith(error, _failure);
            break;
        }
        if (_source == nil && !_opening) {
            SWFail(error, SWErrorNameUnavailable, @"graphics: the screen stream stopped");
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

- (NSInteger)addUnitListener:(void (^)(SWAccessUnit *))listener {
    [_state lock];
    _nextListener += 1;
    NSInteger identifier = _nextListener;
    _unitListeners[@(identifier)] = listener;
    [_state unlock];
    return identifier;
}

- (void)removeListener:(NSInteger)identifier {
    [_state lock];
    [_unitListeners removeObjectForKey:@(identifier)];
    [_state unlock];
}

- (void)onPackets:(NSData *)batch {
    dispatch_async(_queue, ^{
        @autoreleasepool {
            [self process:batch];
        }
    });
}

- (void)onEnd:(NSString *)failure {
    [_state lock];
    _failure = SWErrorFromFailure(failure);
    _source = nil;
    _latest = nil;
    [_state broadcast];
    [_state unlock];
}

- (void)process:(NSData *)batch {
    [_state lock];
    NSArray<void (^)(SWAccessUnit *)> *listeners = _unitListeners.allValues;
    [_state unlock];
    NSMutableArray<SWAccessUnit *> *units = [NSMutableArray array];
    const uint8_t *bytes = batch.bytes;
    NSInteger total = (NSInteger)batch.length;
    NSInteger offset = 0;
    while (offset + SWPacketHeaderSize <= total) {
        uint64_t arrival = 0;
        for (NSInteger index = 0; index < 8; index++) {
            arrival = arrival << 8 | bytes[offset + index];
        }
        BOOL rtcp = bytes[offset + 8] != 0;
        uint32_t length = 0;
        for (NSInteger index = 9; index < SWPacketHeaderSize; index++) {
            length = length << 8 | bytes[offset + index];
        }
        offset += SWPacketHeaderSize;
        if (offset + (NSInteger)length > total) {
            break;
        }
        if (!rtcp) {
            [_depacketizer push:bytes + offset length:length arrival:(double)arrival / 1e6 into:units];
        }
        offset += length;
    }
    for (SWAccessUnit *unit in units) {
        for (void (^listener)(SWAccessUnit *) in listeners) {
            listener(unit);
        }
        if ([_decoder decode:unit]) {
            [self requestKeyFrame];
        }
    }
}

- (void)decoded:(CVPixelBufferRef)image unit:(SWAccessUnit *)unit {
    [_state lock];
    if (_source == nil) {
        [_state unlock];
        return;
    }
    _latest = [[SWFrame alloc] initWithPixelBuffer:image time:unit.arrival turns:unit.turns geometry:_geometry];
    _sequence += 1;
    [_state broadcast];
    [_state unlock];
}

@end

