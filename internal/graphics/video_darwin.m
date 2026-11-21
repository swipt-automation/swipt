//go:build darwin && cgo

#import <AVFoundation/AVFoundation.h>
#import <VideoToolbox/VideoToolbox.h>
#import <stdatomic.h>

#include "_cgo_export.h"
#include "graphics_darwin.h"

#import "error_darwin.h"

void swipt_pixel_buffer_size(void *buffer, int64_t *width, int64_t *height) {
    *width = (int64_t)CVPixelBufferGetWidth((CVPixelBufferRef)buffer);
    *height = (int64_t)CVPixelBufferGetHeight((CVPixelBufferRef)buffer);
}

char *swipt_pixel_buffer_copy(void *buffer, uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes) {
    CVPixelBufferRef image = (CVPixelBufferRef)buffer;
    CVReturn locked = CVPixelBufferLockBaseAddress(image, kCVPixelBufferLock_ReadOnly);
    if (locked != kCVReturnSuccess) {
        return SWFailureStatus(@"graphics: lock the frame", locked);
    }
    const uint8_t *base = CVPixelBufferGetBaseAddress(image);
    size_t stride = CVPixelBufferGetBytesPerRow(image);
    if (base != NULL) {
        for (int64_t row = 0; row < height; row++) {
            memcpy(pixels + row * row_bytes, base + (size_t)row * stride, (size_t)width * 4);
        }
    }
    CVPixelBufferUnlockBaseAddress(image, kCVPixelBufferLock_ReadOnly);
    return base == NULL ? SWFailure(@"graphics: the frame has no pixels") : NULL;
}

CF_RETURNS_RETAINED static CMSampleBufferRef SWHEVCSample(const swipt_graphics_unit *unit, CMVideoFormatDescriptionRef format) {
    size_t size = unit->sample.length;
    if (size == 0) {
        return NULL;
    }
    CMBlockBufferRef block = NULL;
    if (CMBlockBufferCreateWithMemoryBlock(NULL, NULL, size, NULL, NULL, 0, size, kCMBlockBufferAssureMemoryNowFlag, &block) != noErr) {
        return NULL;
    }
    CMSampleBufferRef sample = NULL;
    if (CMBlockBufferReplaceDataBytes(unit->sample.bytes, block, 0, size) == noErr) {
        CMSampleTimingInfo timing = {.duration = kCMTimeInvalid, .presentationTimeStamp = CMTimeMake(unit->time, unit->timescale), .decodeTimeStamp = kCMTimeInvalid};
        CMSampleBufferCreateReady(NULL, block, format, 1, 1, &timing, 1, &size, &sample);
    }
    CFRelease(block);
    if (sample != NULL && !unit->keyframe) {
        CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sample, true);
        if (attachments != NULL && CFArrayGetCount(attachments) > 0) {
            CFMutableDictionaryRef attachment = (CFMutableDictionaryRef)CFArrayGetValueAtIndex(attachments, 0);
            CFDictionarySetValue(attachment, kCMSampleAttachmentKey_NotSync, kCFBooleanTrue);
            CFDictionarySetValue(attachment, kCMSampleAttachmentKey_DependsOnOthers, kCFBooleanTrue);
        }
    }
    return sample;
}

@interface SWFrameDecoder : NSObject
@end

@implementation SWFrameDecoder {
    uintptr_t _stream;
    CMVideoFormatDescriptionRef _format;
    VTDecompressionSessionRef _session;
    BOOL _waitingForKeyframe;
    atomic_bool _failed;
    atomic_int _failedStatus;
    OSStatus _reportedStatus;
}

- (instancetype)initWithStream:(uintptr_t)stream {
    self = [super init];
    if (self) {
        _stream = stream;
        _waitingForKeyframe = YES;
    }
    return self;
}

- (void)dealloc {
    [self close];
}

- (void)report:(char *)message {
    swiptGraphicsScreenLog(_stream, message);
    free(message);
}

- (void)close {
    [self closeSession];
    if (_format != NULL) {
        CFRelease(_format);
        _format = NULL;
    }
}

- (void)closeSession {
    if (_session != NULL) {
        VTDecompressionSessionWaitForAsynchronousFrames(_session);
        VTDecompressionSessionInvalidate(_session);
        CFRelease(_session);
        _session = NULL;
    }
}

- (void)invalidate {
    [self closeSession];
    _waitingForKeyframe = YES;
}

- (BOOL)makeSession:(CMVideoFormatDescriptionRef)format {
    [self closeSession];
    NSDictionary *attributes = @{
        (__bridge id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA),
        (__bridge id)kCVPixelBufferIOSurfacePropertiesKey: @{},
    };
    VTDecompressionSessionRef created = NULL;
    OSStatus status = VTDecompressionSessionCreate(NULL, format, NULL, (__bridge CFDictionaryRef)attributes, NULL, &created);
    if (status != noErr || created == NULL) {
        [self report:SWFailureStatus(@"create the screen decoder", status)];
        return NO;
    }
    VTSessionSetProperty(created, kVTDecompressionPropertyKey_RealTime, kCFBooleanTrue);
    _session = created;
    return YES;
}

- (void)reportStatus:(OSStatus)status {
    if (status != _reportedStatus) {
        _reportedStatus = status;
        [self report:SWFailureStatus(@"decode the screen", status)];
    }
}

- (BOOL)adoptParameterSets:(const swipt_graphics_unit *)unit {
    const uint8_t *pointers[3] = {unit->vps.bytes, unit->sps.bytes, unit->pps.bytes};
    const size_t sizes[3] = {unit->vps.length, unit->sps.length, unit->pps.length};
    CMVideoFormatDescriptionRef described = NULL;
    OSStatus status = CMVideoFormatDescriptionCreateFromHEVCParameterSets(NULL, 3, pointers, sizes, 4, NULL, &described);
    if (status != noErr) {
        [self report:SWFailureStatus(@"read the HEVC parameter sets", status)];
        return NO;
    }
    BOOL adopted = YES;
    if (_format == NULL || !CMFormatDescriptionEqual(_format, described) || _session == NULL) {
        if (_format != NULL) {
            CFRelease(_format);
        }
        _format = (CMVideoFormatDescriptionRef)CFRetain(described);
        if (_session == NULL || !VTDecompressionSessionCanAcceptFormatDescription(_session, described)) {
            adopted = [self makeSession:described];
        }
    }
    CFRelease(described);
    return adopted;
}

- (BOOL)decode:(const swipt_graphics_unit *)unit arrival:(double)arrival turns:(int32_t)turns {
    if (atomic_exchange(&_failed, false)) {
        [self reportStatus:atomic_load(&_failedStatus)];
        [self invalidate];
    }
    if (unit->damaged) {
        _waitingForKeyframe = YES;
        return YES;
    }
    if (unit->sps.length > 0 && ![self adoptParameterSets:unit]) {
        return YES;
    }
    if (_waitingForKeyframe && !unit->keyframe) {
        return YES;
    }
    if (_session == NULL && _format != NULL && ![self makeSession:_format]) {
        return YES;
    }
    if (_session == NULL || _format == NULL) {
        return YES;
    }
    _waitingForKeyframe = NO;
    CMSampleBufferRef sample = SWHEVCSample(unit, _format);
    if (sample == NULL) {
        return NO;
    }
    uintptr_t stream = _stream;
    __weak SWFrameDecoder *weakSelf = self;
    VTDecodeInfoFlags info = 0;
    OSStatus status = VTDecompressionSessionDecodeFrameWithOutputHandler(_session, sample, kVTDecodeFrame_1xRealTimePlayback | kVTDecodeFrame_EnableAsynchronousDecompression, &info,
        ^(OSStatus decoded, VTDecodeInfoFlags flags, CVImageBufferRef image, CMTime time, CMTime duration) {
            SWFrameDecoder *decoder = weakSelf;
            if (decoder == nil) {
                return;
            }
            if (decoded != noErr || image == NULL) {
                atomic_store(&decoder->_failedStatus, decoded);
                atomic_store(&decoder->_failed, true);
                return;
            }
            swiptGraphicsScreenFrame(stream, (void *)CVPixelBufferRetain(image), arrival, turns);
        });
    CFRelease(sample);
    if (status != noErr) {
        [self reportStatus:status];
        [self invalidate];
        return YES;
    }
    return NO;
}

@end

void *swipt_decoder_create(uintptr_t stream) {
    @autoreleasepool {
        return (void *)CFBridgingRetain([[SWFrameDecoder alloc] initWithStream:stream]);
    }
}

bool swipt_decoder_decode(void *decoder, swipt_graphics_unit unit, double arrival, int32_t turns) {
    @autoreleasepool {
        return [(__bridge SWFrameDecoder *)decoder decode:&unit arrival:arrival turns:turns];
    }
}

void swipt_decoder_close(void *decoder) {
    @autoreleasepool {
        SWFrameDecoder *closing = CFBridgingRelease(decoder);
        [closing close];
    }
}

@interface SWDecodedBuffer : NSObject
@property (nonatomic, readonly) double time;
@property (nonatomic, readonly) CVPixelBufferRef buffer NS_RETURNS_INNER_POINTER;
@end

@implementation SWDecodedBuffer

- (instancetype)initWithSample:(CMSampleBufferRef)sample image:(CVImageBufferRef)image {
    self = [super init];
    if (self) {
        _time = CMTimeGetSeconds(CMSampleBufferGetPresentationTimeStamp(sample));
        _buffer = CVPixelBufferRetain(image);
    }
    return self;
}

- (void)dealloc {
    CVPixelBufferRelease(_buffer);
}

@end

@interface SWMovieReader : NSObject
@end

@implementation SWMovieReader {
    AVURLAsset *_asset;
    AVAssetTrack *_track;
    NSLock *_lock;
    AVAssetReader *_reader;
    AVAssetReaderTrackOutput *_output;
    SWDecodedBuffer *_current;
    SWDecodedBuffer *_pending;
}

- (instancetype)initWithAsset:(AVURLAsset *)asset track:(AVAssetTrack *)track {
    self = [super init];
    if (self) {
        _asset = asset;
        _track = track;
        _lock = [NSLock new];
    }
    return self;
}

- (AVAssetReader *)openAt:(double)seconds failure:(char **)failure {
    NSError *opening = nil;
    AVAssetReader *reader = [[AVAssetReader alloc] initWithAsset:_asset error:&opening];
    if (reader == nil) {
        *failure = SWFailureCause(@"graphics: read the video", opening);
        return nil;
    }
    reader.timeRange = CMTimeRangeMake(CMTimeMakeWithSeconds(MAX(seconds - 1, 0), 600), kCMTimePositiveInfinity);
    AVAssetReaderTrackOutput *output = [[AVAssetReaderTrackOutput alloc] initWithTrack:_track outputSettings:@{(__bridge id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA)}];
    output.alwaysCopiesSampleData = NO;
    if (![reader canAddOutput:output]) {
        *failure = SWFailure(@"graphics: read the video");
        return nil;
    }
    [reader addOutput:output];
    if (![reader startReading]) {
        *failure = SWFailureCause(@"graphics: read the video", reader.error);
        return nil;
    }
    return reader;
}

- (SWDecodedBuffer *)next {
    while (YES) {
        CMSampleBufferRef sample = [_output copyNextSampleBuffer];
        if (sample == NULL) {
            return nil;
        }
        CVImageBufferRef image = CMSampleBufferGetImageBuffer(sample);
        SWDecodedBuffer *decoded = image == NULL ? nil : [[SWDecodedBuffer alloc] initWithSample:sample image:image];
        CFRelease(sample);
        if (decoded != nil) {
            return decoded;
        }
    }
}

- (char *)frameLockedAt:(double)seconds token:(SWCancelToken *)cancel buffer:(void **)buffer {
    BOOL reusable = _current != nil && _current.time <= seconds && seconds - _current.time < 2;
    if (!reusable) {
        [_reader cancelReading];
        _current = nil;
        _pending = nil;
        char *failure = NULL;
        AVAssetReader *reader = [self openAt:seconds failure:&failure];
        if (reader == nil) {
            return failure;
        }
        _reader = reader;
        _output = (AVAssetReaderTrackOutput *)reader.outputs.firstObject;
    }
    if (_pending != nil && _pending.time <= seconds + 0.0005) {
        _current = _pending;
        _pending = nil;
    }
    while (_pending == nil) {
        if (cancel != nil && cancel.isCancelled) {
            [_reader cancelReading];
            _current = nil;
            return SWFailure(@"graphics: the operation was aborted");
        }
        @autoreleasepool {
            SWDecodedBuffer *sample = [self next];
            if (sample == nil) {
                break;
            }
            if (sample.time <= seconds + 0.0005) {
                _current = sample;
            } else {
                _pending = sample;
            }
        }
    }
    if (_reader.status == AVAssetReaderStatusFailed) {
        char *failure = SWFailureCause(@"graphics: read the video", _reader.error);
        _current = nil;
        _pending = nil;
        return failure;
    }
    SWDecodedBuffer *shown = _current ?: _pending;
    if (shown == nil) {
        return SWFailure([NSString stringWithFormat:@"graphics: the video has no frame at %ld ms", (long)(seconds * 1000)]);
    }
    *buffer = (void *)CVPixelBufferRetain(shown.buffer);
    return NULL;
}

- (char *)frameAt:(double)seconds token:(SWCancelToken *)cancel buffer:(void **)buffer {
    [_lock lock];
    char *failure = [self frameLockedAt:seconds token:cancel buffer:buffer];
    [_lock unlock];
    return failure;
}

@end

char *swipt_movie_open(const char *path, void **reader) {
    @autoreleasepool {
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:[NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:NO] options:nil];
        dispatch_semaphore_t loaded = dispatch_semaphore_create(0);
        __block NSArray<AVAssetTrack *> *tracks = nil;
        __block NSError *failure = nil;
        [asset loadTracksWithMediaType:AVMediaTypeVideo completionHandler:^(NSArray<AVAssetTrack *> *found, NSError *loadError) {
            tracks = found;
            failure = loadError;
            dispatch_semaphore_signal(loaded);
        }];
        dispatch_semaphore_wait(loaded, DISPATCH_TIME_FOREVER);
        NSString *name = asset.URL.lastPathComponent;
        if (failure != nil) {
            return SWFailureCause([NSString stringWithFormat:@"graphics: read %@", name], failure);
        }
        if (tracks.firstObject == nil) {
            return SWFailure([NSString stringWithFormat:@"graphics: %@ has no video track", name]);
        }
        *reader = (void *)CFBridgingRetain([[SWMovieReader alloc] initWithAsset:asset track:tracks.firstObject]);
        return NULL;
    }
}

char *swipt_movie_frame(void *reader, double seconds, void *token, void **buffer) {
    @autoreleasepool {
        return [(__bridge SWMovieReader *)reader frameAt:seconds token:(__bridge SWCancelToken *)token buffer:buffer];
    }
}

@interface SWWriterObserver : NSObject
- (instancetype)initWithChanged:(dispatch_block_t)changed;
@end

@implementation SWWriterObserver {
    dispatch_block_t _changed;
}

- (instancetype)initWithChanged:(dispatch_block_t)changed {
    self = [super init];
    if (self) {
        _changed = [changed copy];
    }
    return self;
}

- (void)observeValueForKeyPath:(NSString *)keyPath ofObject:(id)object change:(NSDictionary<NSKeyValueChangeKey, id> *)change context:(void *)context {
    _changed();
}

@end

@interface SWMovieEncoder : NSObject
- (instancetype)initWithWriter:(AVAssetWriter *)writer input:(AVAssetWriterInput *)input adaptor:(AVAssetWriterInputPixelBufferAdaptor *)adaptor token:(SWCancelToken *)token;
- (char *)append:(const uint8_t *)pixels rowBytes:(int64_t)rowBytes milliseconds:(double)milliseconds;
- (char *)finish;
- (void)cancel;
@end

@implementation SWMovieEncoder {
    AVAssetWriter *_writer;
    AVAssetWriterInput *_input;
    AVAssetWriterInputPixelBufferAdaptor *_adaptor;
    SWCancelToken *_token;
    dispatch_queue_t _queue;
    dispatch_semaphore_t _frameDone;
    SWWriterObserver *_observer;
    void (^_stopCancel)(void);
    BOOL _observing;
    BOOL _ended;
    BOOL _hasPending;
    const uint8_t *_pendingPixels;
    int64_t _pendingRowBytes;
    CMTime _pendingTime;
    char *_pendingFailure;
}

- (instancetype)initWithWriter:(AVAssetWriter *)writer input:(AVAssetWriterInput *)input adaptor:(AVAssetWriterInputPixelBufferAdaptor *)adaptor token:(SWCancelToken *)token {
    self = [super init];
    if (self) {
        _writer = writer;
        _input = input;
        _adaptor = adaptor;
        _token = token;
        dispatch_queue_t queue = dispatch_queue_create("swipt.encoder", DISPATCH_QUEUE_SERIAL);
        _queue = queue;
        _frameDone = dispatch_semaphore_create(0);
        __weak SWMovieEncoder *weakSelf = self;
        _observer = [[SWWriterObserver alloc] initWithChanged:^{
            dispatch_async(queue, ^{
                @autoreleasepool {
                    [weakSelf drain];
                }
            });
        }];
        dispatch_sync(queue, ^{
            [self->_writer addObserver:self->_observer forKeyPath:@"status" options:NSKeyValueObservingOptionNew context:NULL];
            self->_observing = YES;
            [self->_input requestMediaDataWhenReadyOnQueue:queue usingBlock:^{
                @autoreleasepool {
                    [weakSelf drain];
                }
            }];
        });
        if (token != nil) {
            _stopCancel = [token whenCancelled:^{
                dispatch_async(queue, ^{
                    @autoreleasepool {
                        [weakSelf drain];
                    }
                });
            }];
        }
    }
    return self;
}

- (void)drain {
    if (!_hasPending) {
        return;
    }
    if (_ended || (_token != nil && _token.isCancelled)) {
        _pendingFailure = SWFailure(@"graphics: the operation was aborted");
        _pendingPixels = NULL;
        _hasPending = NO;
        dispatch_semaphore_signal(_frameDone);
        return;
    }
    if (_writer.status != AVAssetWriterStatusWriting) {
        _pendingFailure = SWFailureCause(@"graphics: write the video", _writer.error);
        _pendingPixels = NULL;
        _hasPending = NO;
        dispatch_semaphore_signal(_frameDone);
        return;
    }
    if (!_input.isReadyForMoreMediaData) {
        return;
    }
    CVPixelBufferPoolRef pool = _adaptor.pixelBufferPool;
    if (pool == NULL) {
        _pendingFailure = SWFailureCause(@"graphics: the video writer has no buffers", _writer.error);
        _pendingPixels = NULL;
        _hasPending = NO;
        dispatch_semaphore_signal(_frameDone);
        return;
    }
    CVPixelBufferRef buffer = NULL;
    CVReturn allocated = CVPixelBufferPoolCreatePixelBuffer(NULL, pool, &buffer);
    if (buffer == NULL) {
        _pendingFailure = SWFailureStatus(@"graphics: allocate a video frame", allocated);
        _pendingPixels = NULL;
        _hasPending = NO;
        dispatch_semaphore_signal(_frameDone);
        return;
    }
    CVPixelBufferLockBaseAddress(buffer, 0);
    uint8_t *base = CVPixelBufferGetBaseAddress(buffer);
    size_t stride = CVPixelBufferGetBytesPerRow(buffer);
    size_t width = CVPixelBufferGetWidth(buffer);
    size_t height = CVPixelBufferGetHeight(buffer);
    for (size_t row = 0; base != NULL && row < height; row++) {
        memcpy(base + row * stride, _pendingPixels + (int64_t)row * _pendingRowBytes, width * 4);
    }
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    BOOL appended = [_adaptor appendPixelBuffer:buffer withPresentationTime:_pendingTime];
    CVPixelBufferRelease(buffer);
    if (!appended) {
        _pendingFailure = SWFailureCause(@"graphics: write the video", _writer.error);
    }
    _pendingPixels = NULL;
    _hasPending = NO;
    dispatch_semaphore_signal(_frameDone);
}

- (char *)append:(const uint8_t *)pixels rowBytes:(int64_t)rowBytes milliseconds:(double)milliseconds {
    dispatch_async(_queue, ^{
        @autoreleasepool {
            self->_pendingPixels = pixels;
            self->_pendingRowBytes = rowBytes;
            self->_pendingTime = CMTimeMake((int64_t)round(milliseconds), 1000);
            self->_pendingFailure = NULL;
            self->_hasPending = YES;
            [self drain];
        }
    });
    dispatch_semaphore_wait(_frameDone, DISPATCH_TIME_FOREVER);
    return _pendingFailure;
}

- (void)stopObserving {
    if (_stopCancel != nil) {
        _stopCancel();
        _stopCancel = nil;
    }
    dispatch_sync(_queue, ^{
        self->_ended = YES;
        if (self->_observing) {
            [self->_writer removeObserver:self->_observer forKeyPath:@"status"];
            self->_observing = NO;
        }
    });
}

- (char *)finish {
    [self stopObserving];
    dispatch_semaphore_t finished = dispatch_semaphore_create(0);
    dispatch_sync(_queue, ^{
        [self->_input markAsFinished];
        [self->_writer finishWritingWithCompletionHandler:^{
            dispatch_semaphore_signal(finished);
        }];
    });
    dispatch_semaphore_wait(finished, DISPATCH_TIME_FOREVER);
    return _writer.status == AVAssetWriterStatusCompleted ? NULL : SWFailureCause(@"graphics: finish the video", _writer.error);
}

- (void)cancel {
    [self stopObserving];
    dispatch_sync(_queue, ^{
        [self->_writer cancelWriting];
    });
}

@end

char *swipt_encoder_create(const char *path, bool mp4, int64_t width, int64_t height, void *token, void **encoder) {
    @autoreleasepool {
        SWCancelToken *cancel = (__bridge SWCancelToken *)token;
        if (cancel != nil && cancel.isCancelled) {
            return SWFailure(@"graphics: the operation was aborted");
        }
        NSURL *target = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:NO];
        NSError *creating = nil;
        AVAssetWriter *writer = [[AVAssetWriter alloc] initWithURL:target fileType:mp4 ? AVFileTypeMPEG4 : AVFileTypeQuickTimeMovie error:&creating];
        if (writer == nil) {
            return SWFailureCause(@"graphics: create the video", creating);
        }
        AVAssetWriterInput *input = [[AVAssetWriterInput alloc] initWithMediaType:AVMediaTypeVideo outputSettings:@{
            AVVideoCodecKey: mp4 ? AVVideoCodecTypeH264 : AVVideoCodecTypeHEVC,
            AVVideoWidthKey: @(width),
            AVVideoHeightKey: @(height),
        }];
        input.expectsMediaDataInRealTime = NO;
        AVAssetWriterInputPixelBufferAdaptor *adaptor = [[AVAssetWriterInputPixelBufferAdaptor alloc] initWithAssetWriterInput:input sourcePixelBufferAttributes:@{
            (__bridge id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA),
            (__bridge id)kCVPixelBufferWidthKey: @(width),
            (__bridge id)kCVPixelBufferHeightKey: @(height),
        }];
        if (![writer canAddInput:input]) {
            return SWFailure(@"graphics: create the video writer");
        }
        [writer addInput:input];
        if (![writer startWriting]) {
            return SWFailureCause(@"graphics: start the video", writer.error);
        }
        [writer startSessionAtSourceTime:kCMTimeZero];
        *encoder = (void *)CFBridgingRetain([[SWMovieEncoder alloc] initWithWriter:writer input:input adaptor:adaptor token:cancel]);
        return NULL;
    }
}

char *swipt_encoder_append(void *encoder, const uint8_t *pixels, int64_t row_bytes, double milliseconds) {
    @autoreleasepool {
        return [(__bridge SWMovieEncoder *)encoder append:pixels rowBytes:row_bytes milliseconds:milliseconds];
    }
}

char *swipt_encoder_finish(void *encoder) {
    @autoreleasepool {
        SWMovieEncoder *movie = CFBridgingRelease(encoder);
        return [movie finish];
    }
}

void swipt_encoder_cancel(void *encoder) {
    @autoreleasepool {
        SWMovieEncoder *movie = CFBridgingRelease(encoder);
        [movie cancel];
    }
}
