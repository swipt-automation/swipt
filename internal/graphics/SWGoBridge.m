//go:build darwin && cgo

#import "SWGoBridge.h"

#include "_cgo_export.h"

#import "SWError.h"
#import "SWScreenFrames.h"

static char *SWCString(NSString *text) {
    return (char *)text.UTF8String;
}

static NSError *SWTakeFailure(char *failure) {
    NSError *error = SWErrorFromFailure([NSString stringWithUTF8String:failure]);
    free(failure);
    return error;
}

static NSURL *SWTakeFileURL(char *path, BOOL directory) {
    NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:directory];
    free(path);
    return url;
}

@implementation SWGoObject

- (instancetype)initWithHandle:(uintptr_t)handle {
    self = [super init];
    if (self) {
        _handle = handle;
    }
    return self;
}

- (void)dealloc {
    swiptGraphicsRelease(_handle);
}

@end

@interface SWGoFile ()
@property (nonatomic, readwrite) NSURL *url;
@end

@implementation SWGoFile

- (BOOL)commit:(NSError **)error {
    char *failure = swiptGraphicsCommitFile(self.handle);
    if (failure == NULL) {
        return YES;
    }
    SWFailWith(error, SWTakeFailure(failure));
    return NO;
}

- (void)discard {
    swiptGraphicsDiscardFile(self.handle);
}

@end

@implementation SWCallContext

- (BOOL)cancelled {
    return swiptGraphicsCancelled(self.handle);
}

- (NSData *)readRoot:(NSString *)root path:(NSString *)path error:(NSError **)error {
    swipt_graphics_file file = swiptGraphicsReadFile(self.handle, SWCString(root), SWCString(path));
    if (file.failure != NULL) {
        SWFailWith(error, SWTakeFailure(file.failure));
        return nil;
    }
    return [NSData dataWithBytesNoCopy:file.bytes length:file.length freeWhenDone:YES];
}

- (BOOL)writeData:(NSData *)data root:(NSString *)root path:(NSString *)path error:(NSError **)error {
    char *failure = swiptGraphicsWriteFile(self.handle, SWCString(root), SWCString(path), (void *)data.bytes, data.length);
    if (failure == NULL) {
        return YES;
    }
    SWFailWith(error, SWTakeFailure(failure));
    return NO;
}

- (NSURL *)directoryAtRoot:(NSString *)root path:(NSString *)path create:(BOOL)create error:(NSError **)error {
    swipt_graphics_directory directory = swiptGraphicsDirectory(self.handle, SWCString(root), SWCString(path), create);
    if (directory.failure != NULL) {
        SWFailWith(error, SWTakeFailure(directory.failure));
        return nil;
    }
    return SWTakeFileURL(directory.path, YES);
}

- (SWGoFile *)stageFileAtRoot:(NSString *)root path:(NSString *)path error:(NSError **)error {
    swipt_graphics_staged_file staged = swiptGraphicsStageFile(self.handle, SWCString(root), SWCString(path));
    if (staged.failure != NULL) {
        SWFailWith(error, SWTakeFailure(staged.failure));
        return nil;
    }
    SWGoFile *file = [[SWGoFile alloc] initWithHandle:staged.file];
    file.url = SWTakeFileURL(staged.path, NO);
    return file;
}

- (NSData *)screenshotWithScale:(double *)scale error:(NSError **)error {
    swipt_graphics_screenshot shot = swiptGraphicsScreenshot(self.handle);
    if (shot.failure != NULL) {
        SWFailWith(error, SWTakeFailure(shot.failure));
        return nil;
    }
    *scale = shot.scale;
    return [NSData dataWithBytesNoCopy:shot.image length:shot.length freeWhenDone:YES];
}

- (void)publish:(NSString *)value {
    swiptGraphicsPublish(self.handle, SWCString(value));
}

@end

@implementation SWGoResource

- (void)relinquish {
    swiptGraphicsReleaseObject(self.handle);
}

@end

@implementation SWRecordingSession

- (BOOL)stop:(NSError **)error {
    char *failure = swiptGraphicsStopRecording(self.handle);
    if (failure == NULL) {
        return YES;
    }
    SWFailWith(error, SWTakeFailure(failure));
    return NO;
}

@end

@implementation SWScreenOwner

- (instancetype)init {
    return [super initWithHandle:swiptGraphicsNewScreenOwner()];
}

- (void)close {
    swiptGraphicsCloseScreenOwner(self.handle);
}

@end

@implementation SWScreenStream {
    SWScreenFrames *_frames;
}

+ (instancetype)openWithContext:(SWCallContext *)context owner:(SWScreenOwner *)owner mode:(NSString *)mode maximumBitrate:(uint64_t)maximumBitrate error:(NSError **)error {
    swipt_graphics_screen_stream screen = swiptGraphicsOpenScreen(context.handle, owner.handle, SWCString(mode), maximumBitrate);
    SWScreenFrames *frames = (__bridge_transfer SWScreenFrames *)screen.frames;
    if (screen.failure != NULL) {
        SWFailWith(error, SWTakeFailure(screen.failure));
        return nil;
    }
    SWScreenStream *stream = [[self alloc] initWithHandle:screen.stream];
    stream->_frames = frames;
    return stream;
}

- (BOOL)closed {
    return swiptGraphicsScreenClosed(self.handle);
}

- (SWFrame *)latestFrame:(SWCancelToken *)token error:(NSError **)error {
    NSInteger sequence = 0;
    NSError *failure = nil;
    NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:SWIPT_GRAPHICS_FIRST_FRAME_SECONDS];
    SWFrame *frame = [self frameAfter:0 until:deadline token:token sequence:&sequence error:&failure];
    if (frame == nil) {
        SWFailWith(error, failure ?: SWTakeFailure(swiptGraphicsScreenFailure(self.handle)));
    }
    return frame;
}

- (SWFrame *)frameAfter:(NSInteger)previous until:(NSDate *)deadline token:(SWCancelToken *)token sequence:(NSInteger *)sequence error:(NSError **)error {
    return [_frames frameAfter:previous until:deadline token:token closed:^BOOL {
        return self.closed;
    } sequence:sequence error:error];
}

- (SWRecordingSession *)recordWithContext:(SWCallContext *)context root:(NSString *)root path:(NSString *)path error:(NSError **)error {
    swipt_graphics_recording_session recording = swiptGraphicsRecordScreen(context.handle, self.handle, SWCString(root), SWCString(path));
    if (recording.failure != NULL) {
        SWFailWith(error, SWTakeFailure(recording.failure));
        return nil;
    }
    return [[SWRecordingSession alloc] initWithHandle:recording.session];
}

- (void)close {
    swiptGraphicsCloseScreen(self.handle);
}

@end

void SWLogError(NSString *message) {
    swiptGraphicsLogError(SWCString(message));
}
