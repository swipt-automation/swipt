//go:build darwin && cgo

#import "SWRecordingSession.h"

#import <CoreMedia/CoreMedia.h>

#import "SWAccessUnit.h"
#import "SWCancelToken.h"
#import "SWError.h"
#import "SWGoBridge.h"
#import "SWHEVC.h"
#import "SWJSON.h"
#import "SWMovieWriter.h"
#import "SWScreenRecording.h"
#import "SWScreenStream.h"

@interface SWRecordingSegment : NSObject
@property (nonatomic, readonly) NSString *file;
@property (nonatomic, readonly) double start;
@property (nonatomic, readonly) NSInteger turns;
@end

@implementation SWRecordingSegment

- (instancetype)initWithFile:(NSString *)file start:(double)start turns:(NSInteger)turns {
    self = [super init];
    if (self) {
        _file = file;
        _start = start;
        _turns = turns;
    }
    return self;
}

@end

@implementation SWRecordingSession {
    NSURL *_directory;
    SWScreenStream *_stream;
    SWScreenGeometry _geometry;
    NSLock *_lock;
    NSInteger _listener;
    dispatch_group_t _finishing;
    dispatch_semaphore_t _began;
    BOOL _hasOrigin;
    double _origin;
    double _last;
    NSMutableArray<SWRecordingSegment *> *_segments;
    SWMovieWriter *_writer;
    CMVideoFormatDescriptionRef _format;
    BOOL _waiting;
    BOOL _stopped;
    NSMutableArray<NSString *> *_failures;
}

+ (instancetype)startInDirectory:(NSURL *)directory stream:(SWScreenStream *)stream token:(SWCancelToken *)token error:(NSError **)error {
    NSArray *existing = [NSFileManager.defaultManager contentsOfDirectoryAtPath:directory.path error:error];
    if (existing == nil) {
        return nil;
    }
    if (existing.count > 0) {
        SWFail(error, SWErrorNameState, @"graphics: %@ is not empty", directory.lastPathComponent);
        return nil;
    }
    SWRecordingSession *session = [self new];
    session->_directory = directory;
    session->_stream = stream;
    session->_geometry = stream.geometry;
    session->_lock = [NSLock new];
    session->_finishing = dispatch_group_create();
    session->_began = dispatch_semaphore_create(0);
    session->_segments = [NSMutableArray array];
    session->_failures = [NSMutableArray array];
    session->_waiting = YES;
    __weak SWRecordingSession *weakSession = session;
    session->_listener = [stream addUnitListener:^(SWAccessUnit *unit) {
        [weakSession append:unit];
    }];
    if (![stream addRecording:session]) {
        [stream removeListener:session->_listener];
        SWFail(error, SWErrorNameState, @"graphics: the screen stream is closed");
        return nil;
    }
    [stream requestKeyFrame];
    NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:SWScreenFirstFrameLimit];
    while (dispatch_semaphore_wait(session->_began, dispatch_time(DISPATCH_TIME_NOW, 50 * NSEC_PER_MSEC)) != 0) {
        NSError *failure = nil;
        if (![token failIfCancelled:&failure] && [deadline timeIntervalSinceNow] <= 0) {
            failure = SWMakeError(SWErrorNameUnavailable, [NSString stringWithFormat:@"graphics: the screen sent no frame within %ld s; it may be off", (long)SWScreenFirstFrameLimit]);
        }
        if (failure != nil) {
            [session stop:nil];
            SWFailWith(error, failure);
            return nil;
        }
    }
    return session;
}

- (void)dealloc {
    if (_format != NULL) {
        CFRelease(_format);
    }
}

- (void)append:(SWAccessUnit *)unit {
    [_lock lock];
    [self appendLocked:unit];
    [_lock unlock];
}

- (void)appendLocked:(SWAccessUnit *)unit {
    if (_stopped) {
        return;
    }
    if (unit.damaged) {
        _waiting = YES;
        return;
    }
    if (unit.hasParameterSets) {
        CMVideoFormatDescriptionRef described = SWHEVCFormat(unit.nals);
        if (described != NULL) {
            if (_format != NULL) {
                CFRelease(_format);
            }
            _format = described;
        }
    }
    BOOL changed = _writer == nil || (_format != NULL && (_writer.turns != unit.turns || !CMFormatDescriptionEqual(_format, _writer.format)));
    if (changed) {
        if (_writer != nil) {
            [self closeWriter:_writer];
            _writer = nil;
        }
        if (!unit.isKeyframe || _format == NULL) {
            _waiting = YES;
            return;
        }
        if (!_hasOrigin) {
            _hasOrigin = YES;
            _origin = unit.arrival;
            dispatch_semaphore_signal(_began);
        }
        NSString *file = [NSString stringWithFormat:@"%lu.mov", (unsigned long)_segments.count + 1];
        NSError *failure = nil;
        SWMovieWriter *created = [[SWMovieWriter alloc] initWithURL:[_directory URLByAppendingPathComponent:file] format:_format turns:unit.turns error:&failure];
        if (created == nil) {
            [_failures addObject:SWErrorMessageOf(failure)];
            return;
        }
        _writer = created;
        [_segments addObject:[[SWRecordingSegment alloc] initWithFile:file start:unit.arrival - _origin turns:unit.turns]];
        _waiting = NO;
    }
    if (_waiting) {
        if (!unit.isKeyframe) {
            return;
        }
        _waiting = NO;
    }
    [_writer append:unit];
    _last = unit.arrival;
}

- (void)closeWriter:(SWMovieWriter *)writer {
    dispatch_group_enter(_finishing);
    [writer finish:^(NSString *failure) {
        if (failure != nil) {
            [self->_lock lock];
            [self->_failures addObject:failure];
            [self->_lock unlock];
        }
        dispatch_group_leave(self->_finishing);
    }];
}

- (BOOL)stop:(NSError **)error {
    [_lock lock];
    if (_stopped) {
        [_lock unlock];
        SWFail(error, SWErrorNameState, @"graphics: the recording already stopped");
        return NO;
    }
    _stopped = YES;
    SWMovieWriter *writer = _writer;
    _writer = nil;
    [_lock unlock];
    return [self finishWriter:writer error:error];
}

- (BOOL)finishWriter:(SWMovieWriter *)writer error:(NSError **)error {
    [_stream removeListener:_listener];
    [_stream removeRecording:self];
    if (writer != nil) {
        [self closeWriter:writer];
    }
    dispatch_group_wait(_finishing, DISPATCH_TIME_FOREVER);
    return [self save:error];
}

- (void)abandon {
    [_lock lock];
    BOOL running = !_stopped;
    _stopped = YES;
    SWMovieWriter *writer = _writer;
    _writer = nil;
    [_lock unlock];
    if (!running) {
        return;
    }
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        @autoreleasepool {
            NSError *failure = nil;
            if (![self finishWriter:writer error:&failure]) {
                SWReportRecordingFailure(SWErrorMessageOf(failure));
            }
        }
    });
}

- (BOOL)save:(NSError **)error {
    [_lock lock];
    BOOL hasOrigin = _hasOrigin;
    double origin = _origin;
    double last = _last;
    NSArray<SWRecordingSegment *> *segments = [_segments copy];
    NSArray<NSString *> *failures = [_failures copy];
    [_lock unlock];
    if (segments.count == 0 && failures.firstObject != nil) {
        SWFail(error, SWErrorNameGeneric, @"graphics: %@", failures.firstObject);
        return NO;
    }
    if (!hasOrigin || segments.count == 0) {
        SWFail(error, SWErrorNameUnavailable, @"graphics: the recording got no frames; the screen may be off or another app may hold it");
        return NO;
    }
    double duration = MAX(last - origin, 0) + 1000.0 / 60;
    NSMutableArray *described = [NSMutableArray array];
    for (NSUInteger index = 0; index < segments.count; index++) {
        SWRecordingSegment *segment = segments[index];
        double following = index + 1 < segments.count ? segments[index + 1].start : duration;
        [described addObject:@{
            @"file": segment.file, @"start": @(SWRounded(segment.start)), @"duration": @(SWRounded(following - segment.start)), @"turns": @(segment.turns),
        }];
    }
    NSString *encoded = SWEncodeJSON(@{
        @"start": @(SWRounded(origin)), @"duration": @(SWRounded(duration)),
        @"screen": @{@"width": @(_geometry.width), @"height": @(_geometry.height), @"scale": @(_geometry.scale)},
        @"segments": described,
    });
    if (encoded == nil) {
        SWFail(error, SWErrorNameGeneric, @"graphics: encode %@", SWRecordingManifest);
        return NO;
    }
    if (![[encoded dataUsingEncoding:NSUTF8StringEncoding] writeToURL:[_directory URLByAppendingPathComponent:SWRecordingManifest] options:NSDataWritingAtomic error:error]) {
        return NO;
    }
    if (failures.firstObject != nil) {
        SWFail(error, SWErrorNameGeneric, @"graphics: %@", failures.firstObject);
        return NO;
    }
    return YES;
}

@end
