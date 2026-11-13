//go:build darwin && cgo

#import "SWFrameDecoder.h"

#import <VideoToolbox/VideoToolbox.h>
#import <stdatomic.h>

#import "SWError.h"
#import "SWGoBridge.h"

static const int32_t SWMediaTimescale = 24000;

CF_RETURNS_RETAINED static CMVideoFormatDescriptionRef SWHEVCFormat(const swipt_graphics_unit *unit) {
    if (unit->vps.length == 0 || unit->sps.length == 0 || unit->pps.length == 0) {
        return NULL;
    }
    const uint8_t *pointers[3] = {unit->vps.bytes, unit->sps.bytes, unit->pps.bytes};
    const size_t sizes[3] = {unit->vps.length, unit->sps.length, unit->pps.length};
    CMVideoFormatDescriptionRef description = NULL;
    OSStatus status = CMVideoFormatDescriptionCreateFromHEVCParameterSets(NULL, 3, pointers, sizes, 4, NULL, &description);
    if (status != noErr) {
        SWLogError([NSString stringWithFormat:@"read the HEVC parameter sets: %@", SWErrorMessageOf(SWErrorStatus(status))]);
        return NULL;
    }
    return description;
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
        CMSampleTimingInfo timing = {.duration = kCMTimeInvalid, .presentationTimeStamp = CMTimeMake(unit->time, SWMediaTimescale), .decodeTimeStamp = kCMTimeInvalid};
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

@implementation SWFrameDecoder {
    CMVideoFormatDescriptionRef _format;
    VTDecompressionSessionRef _session;
    BOOL _waitingForKeyframe;
    atomic_bool _failed;
    atomic_int _failedStatus;
    OSStatus _reportedStatus;
}

- (instancetype)init {
    self = [super init];
    if (self) {
        _waitingForKeyframe = YES;
    }
    return self;
}

- (void)dealloc {
    [self closeSession];
    if (_format != NULL) {
        CFRelease(_format);
    }
}

- (void)closeSession {
    if (_session != NULL) {
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
        SWLogError([NSString stringWithFormat:@"create the screen decoder: %@", SWErrorMessageOf(SWErrorStatus(status))]);
        return NO;
    }
    VTSessionSetProperty(created, kVTDecompressionPropertyKey_RealTime, kCFBooleanTrue);
    _session = created;
    return YES;
}

- (void)reportStatus:(OSStatus)status {
    if (status != _reportedStatus) {
        _reportedStatus = status;
        SWLogError([NSString stringWithFormat:@"decode the screen: %@", SWErrorMessageOf(SWErrorStatus(status))]);
    }
}

- (BOOL)adoptParameterSets:(const swipt_graphics_unit *)unit {
    CMVideoFormatDescriptionRef described = SWHEVCFormat(unit);
    if (described == NULL) {
        return YES;
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

- (BOOL)decode:(const swipt_graphics_unit *)unit {
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
    double arrival = unit->arrival;
    NSInteger turns = unit->turns;
    __weak SWFrameDecoder *weakSelf = self;
    VTDecodeInfoFlags info = 0;
    OSStatus status = VTDecompressionSessionDecodeFrameWithOutputHandler(_session, sample, kVTDecodeFrame_1xRealTimePlayback | kVTDecodeFrame_EnableAsynchronousDecompression, &info,
        ^(OSStatus decoded, VTDecodeInfoFlags flags, CVImageBufferRef image, CMTime time, CMTime duration) {
            @autoreleasepool {
                SWFrameDecoder *decoder = weakSelf;
                if (decoder == nil) {
                    return;
                }
                if (decoded != noErr || image == NULL) {
                    atomic_store(&decoder->_failedStatus, decoded);
                    atomic_store(&decoder->_failed, true);
                    return;
                }
                decoder.onFrame(image, arrival, turns);
            }
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
