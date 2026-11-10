//go:build darwin && cgo

#import "SWArguments.h"
#import "SWCall.h"
#import "SWCancelToken.h"
#import "SWEngine.h"
#import "SWError.h"
#import "SWGoBridge.h"
#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWRecordingSession.h"
#import "SWScreenRecording.h"
#import "SWScreenStream.h"

id SWCallScreenshot(SWCall *call, NSError **error) {
    double started = SWNowMilliseconds();
    double scale = 0;
    NSData *data = [call.context screenshotWithScale:&scale error:error];
    if (data == nil) {
        return nil;
    }
    if (data.length == 0) {
        SWFail(error, SWErrorNameUnavailable, @"graphics: the device sent no screenshot");
        return nil;
    }
    if ([call.token failIfCancelled:error]) {
        return nil;
    }
    SWImageNode *node = [SWImageNode nodeWithEncoded:data scale:scale > 0 ? @(scale) : nil timestamp:started error:error];
    return node == nil ? nil : @{@"image": @([call.engine registerObject:node])};
}

static SWScreenStream *SWStreamOf(SWCall *call, NSError **error) {
    id object = call.objects.firstObject;
    if (![object isKindOfClass:SWScreenStream.class]) {
        SWFail(error, SWErrorNameType, @"graphics: %@ needs a ScreenStream", call.method);
        return nil;
    }
    return object;
}

id SWCallScreenOpen(SWCall *call, NSError **error) {
    NSNumber *maximumBitrate = [call.arguments number:@"maximumBitrate"];
    SWScreenStream *stream = [SWScreenStream openWithMode:[call.arguments string:@"mode"] maximumBitrate:(uint64_t)maximumBitrate.unsignedLongLongValue error:error];
    if (stream == nil) {
        return nil;
    }
    if ([call.token failIfCancelled:error]) {
        [stream close];
        return nil;
    }
    [call.engine trackStream:stream];
    return @{@"stream": @([call.engine registerObject:stream])};
}

id SWCallScreenClose(SWCall *call, NSError **error) {
    SWScreenStream *stream = SWStreamOf(call, error);
    if (stream == nil) {
        return nil;
    }
    [stream close];
    return @{};
}

id SWCallScreenFrame(SWCall *call, NSError **error) {
    SWScreenStream *stream = SWStreamOf(call, error);
    SWFrame *frame = stream == nil ? nil : [stream latestFrame:call.token error:error];
    return frame == nil ? nil : @{@"image": @([call.engine registerObject:[SWImageNode nodeWithFrame:frame]])};
}

BOOL SWCallScreenFrames(SWCall *call, NSError **error) {
    if (![call.method isEqualToString:@"screen.frames"]) {
        SWFail(error, SWErrorNameType, @"graphics: there is no stream %@", call.method);
        return NO;
    }
    SWScreenStream *stream = SWStreamOf(call, error);
    if (stream == nil) {
        return NO;
    }
    NSInteger sequence = 0;
    NSInteger previous = 0;
    NSError *failure = nil;
    while (failure == nil) {
        @autoreleasepool {
            SWFrame *next = [stream frameAfter:sequence token:call.token sequence:&sequence error:&failure];
            if (next == nil) {
                break;
            }
            NSInteger identifier = [call.engine registerObject:[SWImageNode nodeWithFrame:next]];
            if (previous != 0) {
                [call.engine drop:previous];
            }
            previous = identifier;
            [call.context publish:[NSString stringWithFormat:@"%ld", (long)identifier]];
        }
    }
    if (previous != 0) {
        [call.engine drop:previous];
    }
    SWFailWith(error, failure);
    return failure == nil;
}

id SWCallScreenRecord(SWCall *call, NSError **error) {
    SWScreenStream *stream = SWStreamOf(call, error);
    NSURL *directory = stream == nil ? nil : [call.context directoryAtRoot:[call.arguments string:@"root"] path:[call.arguments string:@"path"] create:YES error:error];
    if (directory == nil) {
        return nil;
    }
    SWRecordingSession *session = [SWRecordingSession startInDirectory:directory stream:stream token:call.token error:error];
    return session == nil ? nil : @{@"session": @([call.engine registerObject:session])};
}

id SWCallRecordingStop(SWCall *call, NSError **error) {
    SWRecordingSession *session = call.objects.firstObject;
    if (![session isKindOfClass:SWRecordingSession.class]) {
        SWFail(error, SWErrorNameType, @"graphics: stop needs a RecordingSession");
        return nil;
    }
    return [session stop:error] ? @{} : nil;
}

id SWCallRecordingOpen(SWCall *call, NSError **error) {
    NSURL *directory = [call.context directoryAtRoot:[call.arguments string:@"root"] path:[call.arguments string:@"path"] create:NO error:error];
    if (directory == nil) {
        return nil;
    }
    SWScreenRecording *recording = [SWScreenRecording openDirectory:directory error:error];
    return recording == nil ? nil : @{@"recording": @([call.engine registerObject:recording])};
}

id SWCallRecordingExport(SWCall *call, NSError **error) {
    SWScreenRecording *recording = [call recording:error];
    if (recording == nil) {
        return nil;
    }
    NSString *path = [call.arguments string:@"path"];
    NSString *extension = path.pathExtension.lowercaseString;
    if (![extension isEqualToString:@"mov"] && ![extension isEqualToString:@"mp4"]) {
        SWFail(error, SWErrorNameType, @"graphics: export to a .mov or .mp4 file");
        return nil;
    }
    SWGoFile *file = [call.context stageFileAtRoot:[call.arguments string:@"root"] path:path error:error];
    if (file == nil) {
        return nil;
    }
    if (![recording exportTo:file.url mov:[extension isEqualToString:@"mov"] token:call.token error:error]) {
        [file discard];
        return nil;
    }
    return [file commit:error] ? @{} : nil;
}
