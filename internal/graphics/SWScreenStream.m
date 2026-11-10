//go:build darwin && cgo

#import "SWScreenStream.h"

#import "SWCancelToken.h"
#import "SWError.h"
#import "SWRecordingSession.h"
#import "SWScreenCapture.h"

const NSTimeInterval SWScreenFirstFrameLimit = 5;
NSString *const SWScreenModeRemoteDesktop = @"remoteDesktop";
NSString *const SWScreenModeCoreDevice = @"coreDevice";

@implementation SWScreenStream {
    NSLock *_lock;
    NSMutableArray<SWRecordingSession *> *_recordings;
}

+ (instancetype)openWithMode:(NSString *)mode maximumBitrate:(uint64_t)maximumBitrate error:(NSError **)error {
    SWScreenStream *stream = [self new];
    stream->_mode = [mode copy] ?: SWScreenModeRemoteDesktop;
    stream->_maximumBitrate = maximumBitrate;
    stream->_lock = [NSLock new];
    stream->_recordings = [NSMutableArray array];
    return [SWScreenCapture.shared addStream:stream geometry:&stream->_geometry error:error] ? stream : nil;
}

- (SWFrame *)latestFrame:(SWCancelToken *)token error:(NSError **)error {
    NSInteger sequence = 0;
    NSError *failure = nil;
    SWFrame *frame = [SWScreenCapture.shared frameAfter:0 stream:self until:[NSDate dateWithTimeIntervalSinceNow:SWScreenFirstFrameLimit] token:token sequence:&sequence error:&failure];
    if (frame != nil) {
        return frame;
    }
    if (failure != nil) {
        SWFailWith(error, failure);
    } else if (self.closed) {
        SWFail(error, SWErrorNameState, @"graphics: the screen stream is closed");
    } else {
        SWFail(error, SWErrorNameUnavailable, @"graphics: the screen sent no frame within %ld s; it may be off", (long)SWScreenFirstFrameLimit);
    }
    return nil;
}

- (SWFrame *)frameAfter:(NSInteger)previous token:(SWCancelToken *)token sequence:(NSInteger *)sequence error:(NSError **)error {
    return [SWScreenCapture.shared frameAfter:previous stream:self until:NSDate.distantFuture token:token sequence:sequence error:error];
}

- (void)requestKeyFrame {
    [SWScreenCapture.shared requestKeyFrame];
}

- (NSInteger)addUnitListener:(void (^)(SWAccessUnit *))listener {
    return [SWScreenCapture.shared addUnitListener:listener];
}

- (void)removeListener:(NSInteger)identifier {
    [SWScreenCapture.shared removeListener:identifier];
}

- (BOOL)addRecording:(SWRecordingSession *)recording {
    [_lock lock];
    BOOL added = !_closed;
    if (added) {
        [_recordings addObject:recording];
    }
    [_lock unlock];
    return added;
}

- (void)removeRecording:(SWRecordingSession *)recording {
    [_lock lock];
    [_recordings removeObjectIdenticalTo:recording];
    [_lock unlock];
}

- (void)close {
    [_lock lock];
    if (_closed) {
        [_lock unlock];
        return;
    }
    _closed = YES;
    NSArray<SWRecordingSession *> *recordings = [_recordings copy];
    [_recordings removeAllObjects];
    [_lock unlock];
    for (SWRecordingSession *recording in recordings) {
        [recording abandon];
    }
    [SWScreenCapture.shared removeStream:self];
}

@end
