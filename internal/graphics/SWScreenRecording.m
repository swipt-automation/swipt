//go:build darwin && cgo

#import "SWScreenRecording.h"

#import <AVFoundation/AVFoundation.h>

#import "SWCancelToken.h"
#import "SWError.h"
#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWRaster.h"
#import "SWMovieReader.h"

NSString *const SWRecordingManifest = @"recording.json";

@interface SWRecordedSegment : NSObject
@property (nonatomic, readonly) NSURL *url;
@property (nonatomic, readonly) double start;
@property (nonatomic, readonly) NSInteger turns;
@end

@implementation SWRecordedSegment

- (instancetype)initWithURL:(NSURL *)url start:(double)start turns:(NSInteger)turns {
    self = [super init];
    if (self) {
        _url = url;
        _start = start;
        _turns = turns;
    }
    return self;
}

@end

typedef SWRaster *(^SWExportFrame)(double time, NSError **error);

static BOOL SWAppendFrame(AVAssetWriter *writer, AVAssetWriterInputPixelBufferAdaptor *adaptor, SWRaster *raster, CMTime time, NSError **error) {
    CVPixelBufferPoolRef pool = adaptor.pixelBufferPool;
    if (pool == NULL) {
        SWFailCause(error, SWErrorNameGeneric, writer.error, @"graphics: the video writer has no buffers");
        return NO;
    }
    CVPixelBufferRef buffer = NULL;
    CVReturn allocated = CVPixelBufferPoolCreatePixelBuffer(NULL, pool, &buffer);
    if (buffer == NULL) {
        SWFailCause(error, SWErrorNameGeneric, SWErrorStatus(allocated), @"graphics: allocate a video frame");
        return NO;
    }
    CVPixelBufferLockBaseAddress(buffer, 0);
    uint8_t *base = CVPixelBufferGetBaseAddress(buffer);
    if (base != NULL) {
        size_t rowBytes = CVPixelBufferGetBytesPerRow(buffer);
        NSInteger width = (NSInteger)CVPixelBufferGetWidth(buffer);
        NSInteger height = (NSInteger)CVPixelBufferGetHeight(buffer);
        NSInteger rows = MIN(raster.height, height);
        NSInteger columns = MIN(raster.width, width);
        NSInteger left = (width - columns) / 2;
        NSInteger top = (height - rows) / 2;
        if (rows != height || columns != width) {
            memset(base, 0, rowBytes * height);
        }
        const uint8_t *source = raster.data;
        for (NSInteger row = 0; row < rows; row++) {
            memcpy(base + (top + row) * rowBytes + left * 4, source + row * raster.rowBytes, columns * 4);
        }
    }
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    BOOL appended = [adaptor appendPixelBuffer:buffer withPresentationTime:time];
    CVPixelBufferRelease(buffer);
    if (!appended) {
        SWFailCause(error, SWErrorNameGeneric, writer.error, @"graphics: write the video");
    }
    return appended;
}

static const double SWExportFrameRate = 30;

static BOOL SWExportMovie(NSURL *target, BOOL mov, NSArray<NSNumber *> *times, SWExportFrame frame, SWCancelToken *token, NSError **error) {
    SWRaster *first = frame(times[0].doubleValue, error);
    if (first == nil) {
        return NO;
    }
    NSError *creating = nil;
    AVAssetWriter *writer = [[AVAssetWriter alloc] initWithURL:target fileType:mov ? AVFileTypeQuickTimeMovie : AVFileTypeMPEG4 error:&creating];
    if (writer == nil) {
        SWFailCause(error, SWErrorNameGeneric, creating, @"graphics: create the video");
        return NO;
    }
    AVAssetWriterInput *input = [[AVAssetWriterInput alloc] initWithMediaType:AVMediaTypeVideo outputSettings:@{
        AVVideoCodecKey: mov ? AVVideoCodecTypeHEVC : AVVideoCodecTypeH264,
        AVVideoWidthKey: @(first.width), AVVideoHeightKey: @(first.height),
    }];
    input.expectsMediaDataInRealTime = NO;
    AVAssetWriterInputPixelBufferAdaptor *adaptor = [[AVAssetWriterInputPixelBufferAdaptor alloc] initWithAssetWriterInput:input sourcePixelBufferAttributes:@{
        (__bridge id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA),
        (__bridge id)kCVPixelBufferWidthKey: @(first.width), (__bridge id)kCVPixelBufferHeightKey: @(first.height),
    }];
    if (![writer canAddInput:input]) {
        SWFail(error, SWErrorNameGeneric, @"graphics: create the video writer");
        return NO;
    }
    [writer addInput:input];
    if (![writer startWriting]) {
        SWFailCause(error, SWErrorNameGeneric, writer.error, @"graphics: start the video");
        return NO;
    }
    [writer startSessionAtSourceTime:kCMTimeZero];
    NSError *failure = nil;
    for (NSUInteger index = 0; index < times.count && failure == nil; index++) {
        @autoreleasepool {
            if ([token failIfCancelled:&failure]) {
                break;
            }
            double time = times[index].doubleValue;
            SWRaster *raster = index == 0 ? first : frame(time, &failure);
            if (raster != nil && (raster.width != first.width || raster.height != first.height)) {
                double scale = MIN((double)first.width / raster.width, (double)first.height / raster.height);
                NSInteger width = MIN(MAX(lround(raster.width * scale), 1), first.width);
                NSInteger height = MIN(MAX(lround(raster.height * scale), 1), first.height);
                raster = SWRasterResized(raster, width, height, &failure);
            }
            if (raster == nil) {
                break;
            }
            while (!input.isReadyForMoreMediaData && failure == nil) {
                if (writer.status != AVAssetWriterStatusWriting) {
                    SWFailCause(&failure, SWErrorNameGeneric, writer.error, @"graphics: write the video");
                } else if (![token failIfCancelled:&failure]) {
                    usleep(2000);
                }
            }
            if (failure == nil) {
                SWAppendFrame(writer, adaptor, raster, CMTimeMake((int64_t)round(time), 1000), &failure);
            }
        }
    }
    if (failure != nil) {
        [writer cancelWriting];
        SWFailWith(error, failure);
        return NO;
    }
    [input markAsFinished];
    dispatch_semaphore_t finished = dispatch_semaphore_create(0);
    [writer finishWritingWithCompletionHandler:^{
        dispatch_semaphore_signal(finished);
    }];
    dispatch_semaphore_wait(finished, DISPATCH_TIME_FOREVER);
    if (writer.status != AVAssetWriterStatusCompleted) {
        SWFailCause(error, SWErrorNameGeneric, writer.error, @"graphics: finish the video");
        return NO;
    }
    return YES;
}

@implementation SWScreenRecording {
    SWScreenGeometry _screen;
    NSArray<SWRecordedSegment *> *_segments;
    NSLock *_lock;
    NSMutableDictionary<NSNumber *, SWMovieReader *> *_readers;
}

+ (instancetype)openDirectory:(NSURL *)directory error:(NSError **)error {
    NSURL *manifest = [directory URLByAppendingPathComponent:SWRecordingManifest];
    NSData *data = [NSData dataWithContentsOfURL:manifest];
    if (data == nil) {
        SWFail(error, SWErrorNameNotFound, @"graphics: %@ has no %@", directory.lastPathComponent, SWRecordingManifest);
        return nil;
    }
    NSError *failure = nil;
    NSDictionary *metadata = [NSJSONSerialization JSONObjectWithData:data options:0 error:&failure];
    if (![metadata isKindOfClass:NSDictionary.class]) {
        SWFailCause(error, SWErrorNameData, failure, @"graphics: read %@", SWRecordingManifest);
        return nil;
    }
    double duration = SWAsNumber(metadata[@"duration"]).doubleValue;
    if (!isfinite(duration) || duration < 0) {
        SWFail(error, SWErrorNameData, @"graphics: %@ has an invalid duration", SWRecordingManifest);
        return nil;
    }
    SWScreenRecording *recording = [self new];
    recording->_start = SWAsNumber(metadata[@"start"]).doubleValue;
    recording->_duration = duration;
    NSDictionary *screen = [metadata[@"screen"] isKindOfClass:NSDictionary.class] ? metadata[@"screen"] : @{};
    NSNumber *scale = SWAsNumber(screen[@"scale"]);
    recording->_scale = scale == nil ? 1 : scale.doubleValue;
    recording->_screen = (SWScreenGeometry){.width = SWAsNumber(screen[@"width"]).integerValue, .height = SWAsNumber(screen[@"height"]).integerValue, .scale = recording->_scale};
    NSMutableArray<SWRecordedSegment *> *segments = [NSMutableArray array];
    NSArray *listed = [metadata[@"segments"] isKindOfClass:NSArray.class] ? metadata[@"segments"] : @[];
    for (NSDictionary *segment in listed) {
        if (![segment isKindOfClass:NSDictionary.class]) {
            continue;
        }
        NSString *file = segment[@"file"];
        if (![file isKindOfClass:NSString.class] || file.length == 0 || [file isEqualToString:@"."] || [file isEqualToString:@".."] || [file containsString:@"/"]) {
            continue;
        }
        [segments addObject:[[SWRecordedSegment alloc] initWithURL:[directory URLByAppendingPathComponent:file] start:SWAsNumber(segment[@"start"]).doubleValue turns:SWAsNumber(segment[@"turns"]).integerValue]];
    }
    if (segments.count == 0) {
        SWFail(error, SWErrorNameData, @"graphics: %@ lists no video", SWRecordingManifest);
        return nil;
    }
    recording->_segments = segments;
    recording->_lock = [NSLock new];
    recording->_readers = [NSMutableDictionary dictionary];
    return recording;
}

- (NSInteger)segmentIndexAt:(double)time {
    NSInteger found = 0;
    for (NSUInteger index = 0; index < _segments.count; index++) {
        if (_segments[index].start <= time) {
            found = (NSInteger)index;
        }
    }
    return found;
}

- (void)pixelSizeAt:(double)time width:(NSInteger *)width height:(NSInteger *)height {
    SWRecordedSegment *segment = _segments[[self segmentIndexAt:time]];
    *width = segment.turns % 2 == 0 ? _screen.width : _screen.height;
    *height = segment.turns % 2 == 0 ? _screen.height : _screen.width;
}

- (SWMovieReader *)reader:(NSInteger)index error:(NSError **)error {
    [_lock lock];
    SWMovieReader *reader = _readers[@(index)];
    if (reader == nil) {
        reader = [[SWMovieReader alloc] initWithURL:_segments[index].url error:error];
        if (reader != nil) {
            _readers[@(index)] = reader;
        }
    }
    [_lock unlock];
    return reader;
}

- (SWRaster *)rasterAt:(double)time token:(SWCancelToken *)token error:(NSError **)error {
    NSInteger index = [self segmentIndexAt:time];
    SWRecordedSegment *segment = _segments[index];
    SWMovieReader *reader = [self reader:index error:error];
    if (reader == nil || [token failIfCancelled:error]) {
        return nil;
    }
    SWDecodedBuffer *decoded = [reader frameAt:MAX(time - segment.start, 0) / 1000 error:error];
    if (decoded == nil) {
        return nil;
    }
    return [[[SWFrame alloc] initWithPixelBuffer:decoded.buffer time:time turns:segment.turns geometry:_screen] raster:error];
}

- (SWImageNode *)nodeAt:(double)time {
    return [SWImageNode nodeWithRecording:self time:MIN(MAX(time, 0), _duration)];
}

- (BOOL)exportTo:(NSURL *)target mov:(BOOL)mov token:(SWCancelToken *)token error:(NSError **)error {
    double step = 1000 / SWExportFrameRate;
    NSMutableArray<NSNumber *> *times = [NSMutableArray array];
    for (NSInteger index = 0; index * step <= _duration; index++) {
        [times addObject:@(index * step)];
    }
    SWExportFrame frame = ^SWRaster *(double time, NSError **failure) {
        SWRaster *raster = [[self nodeAt:time] raster:token error:failure];
        return raster == nil ? nil : SWRasterResized(raster, raster.width & ~1, raster.height & ~1, failure);
    };
    return SWExportMovie(target, mov, times, frame, token, error);
}

- (NSString *)info {
    return SWEncodeJSON(@{@"start": @(SWRounded(_start)), @"duration": @(SWRounded(_duration))}) ?: @"{}";
}

@end

