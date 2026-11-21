//go:build darwin && cgo

#import <CoreML/CoreML.h>
#import <Vision/Vision.h>

#include "_cgo_export.h"
#include "accelerate_darwin.h"
#include "graphics_darwin.h"

#import "error_darwin.h"

static NSArray<NSNumber *> *SWPointJSON(CGPoint point) {
    return @[@(point.x), @(point.y)];
}

static NSArray *SWQuadJSON(VNRectangleObservation *observation) {
    return @[SWPointJSON(observation.topLeft), SWPointJSON(observation.topRight), SWPointJSON(observation.bottomRight), SWPointJSON(observation.bottomLeft)];
}

static char *SWJSONString(id object) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:object options:0 error:NULL];
    return data == nil ? strdup("null") : strndup(data.bytes, data.length);
}

static BOOL SWRestrict(VNRequest *request, NSError **error) {
    if (@available(iOS 17.0, macOS 14.0, *)) {
        NSDictionary<VNComputeStage, NSArray<id<MLComputeDeviceProtocol>> *> *supported = [request supportedComputeStageDevicesAndReturnError:error];
        if (supported == nil) {
            return NO;
        }
        for (VNComputeStage stage in supported) {
            id<MLComputeDeviceProtocol> engine = nil;
            id<MLComputeDeviceProtocol> processor = nil;
            for (id<MLComputeDeviceProtocol> device in supported[stage]) {
                if (engine == nil && [(id)device isKindOfClass:MLNeuralEngineComputeDevice.class]) {
                    engine = device;
                }
                if (processor == nil && [(id)device isKindOfClass:MLCPUComputeDevice.class]) {
                    processor = device;
                }
            }
            id<MLComputeDeviceProtocol> chosen = engine ?: processor;
            if (chosen != nil) {
                [request setComputeDevice:chosen forComputeStage:stage];
            }
        }
    }
    return YES;
}

static NSString *SWMatchLanguage(NSString *preferred, NSArray<NSString *> *supported) {
    if ([supported containsObject:preferred]) {
        return preferred;
    }
    NSLocale *locale = [NSLocale localeWithLocaleIdentifier:preferred];
    NSString *byLanguage = nil;
    for (NSString *language in supported) {
        NSLocale *candidate = [NSLocale localeWithLocaleIdentifier:language];
        if (![candidate.languageCode isEqualToString:locale.languageCode]) {
            continue;
        }
        if (candidate.scriptCode == locale.scriptCode || [candidate.scriptCode isEqualToString:locale.scriptCode]) {
            return language;
        }
        byLanguage = byLanguage ?: language;
    }
    return byLanguage;
}

static NSArray<NSString *> *SWDeviceLanguages(VNRecognizeTextRequest *request, NSError **error) {
    NSArray<NSString *> *supported = [request supportedRecognitionLanguagesAndReturnError:error];
    if (supported == nil) {
        return nil;
    }
    NSMutableArray<NSString *> *chosen = [NSMutableArray array];
    for (NSString *preferred in [NSLocale.preferredLanguages arrayByAddingObject:@"en-US"]) {
        NSString *match = SWMatchLanguage(preferred, supported);
        if (match != nil && ![chosen containsObject:match]) {
            [chosen addObject:match];
        }
    }
    for (NSString *language in chosen) {
        if ([language hasPrefix:@"zh"]) {
            return [chosen containsObject:@"en-US"] ? @[language, @"en-US"] : @[language];
        }
    }
    return chosen;
}

static char *SWPerform(VNRequest *request, const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, void *token) {
    SWCancelToken *cancel = (__bridge SWCancelToken *)token;
    if (cancel != nil && cancel.isCancelled) {
        return SWFailure(@"graphics: the operation was aborted");
    }
    NSError *failure = nil;
    if (!SWRestrict(request, &failure)) {
        return SWFailureCause(@"graphics: choose where Vision runs", failure);
    }
    long status = 0;
    CGImageRef image = swipt_image_create(pixels, (long)width, (long)height, (long)row_bytes, channels, &status);
    if (image == NULL) {
        return SWFailure([NSString stringWithFormat:@"graphics: create the image: vImage error %ld", status]);
    }
    VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithCGImage:image options:@{}];
    void (^stop)(void) = cancel == nil ? nil : [cancel whenCancelled:^{
        [request cancel];
    }];
    if (cancel != nil && cancel.isCancelled) {
        if (stop != nil) {
            stop();
        }
        CGImageRelease(image);
        return SWFailure(@"graphics: the operation was aborted");
    }
    BOOL performed = [handler performRequests:@[request] error:&failure];
    if (stop != nil) {
        stop();
    }
    CGImageRelease(image);
    if (cancel != nil && cancel.isCancelled) {
        return SWFailure(@"graphics: the operation was aborted");
    }
    return performed ? NULL : SWFailureCause([NSString stringWithFormat:@"graphics: run %@", NSStringFromClass(request.class)], failure);
}

void swipt_vision_warm(uintptr_t logger) {
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        @autoreleasepool {
            VNRecognizeTextRequest *request = [VNRecognizeTextRequest new];
            request.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
            NSError *failure = nil;
            NSArray<NSString *> *languages = SWDeviceLanguages(request, &failure);
            char *message = NULL;
            if (languages == nil) {
                message = SWFailureCause(@"warm the text recognizer", failure);
            } else {
                request.recognitionLanguages = languages;
                uint8_t *pixels = calloc(64 * 32, 4);
                message = SWPerform(request, pixels, 64, 32, 64 * 4, 4, NULL);
                free(pixels);
            }
            swiptGraphicsWarmed(logger, message);
            free(message);
        }
    });
}

static NSArray<NSString *> *SWStrings(const char *const *values, size_t count) {
    NSMutableArray<NSString *> *strings = [NSMutableArray arrayWithCapacity:count];
    for (size_t index = 0; index < count; index++) {
        [strings addObject:[NSString stringWithUTF8String:values[index]]];
    }
    return strings;
}

char *swipt_text_recognize(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, swipt_text_options options, void *token, void **text, char **lines) {
    @autoreleasepool {
        VNRecognizeTextRequest *request = [VNRecognizeTextRequest new];
        if (options.fast >= 0) {
            request.recognitionLevel = options.fast ? VNRequestTextRecognitionLevelFast : VNRequestTextRecognitionLevelAccurate;
        }
        if (options.correction >= 0) {
            request.usesLanguageCorrection = options.correction != 0;
        }
        NSError *failure = nil;
        NSArray<NSString *> *languages = options.languages != NULL ? SWStrings(options.languages, options.language_count) : SWDeviceLanguages(request, &failure);
        if (languages == nil) {
            return SWFailureCause(@"graphics: list the languages of the text recognizer", failure);
        }
        request.recognitionLanguages = languages;
        if (options.detect_language >= 0) {
            if (@available(iOS 16.0, macOS 13.0, *)) {
                request.automaticallyDetectsLanguage = options.detect_language != 0;
            }
        }
        if (options.words != NULL) {
            request.customWords = SWStrings(options.words, options.word_count);
        }
        if (options.minimum_height > 0) {
            request.minimumTextHeight = (float)options.minimum_height;
        }
        char *message = SWPerform(request, pixels, width, height, row_bytes, channels, token);
        if (message != NULL) {
            return message;
        }
        NSMutableArray *candidates = [NSMutableArray array];
        NSMutableArray *described = [NSMutableArray array];
        for (VNRecognizedTextObservation *observation in request.results) {
            NSArray<VNRecognizedText *> *top = [observation topCandidates:10];
            if (top.count == 0) {
                continue;
            }
            NSMutableArray *texts = [NSMutableArray array];
            for (VNRecognizedText *candidate in top) {
                [texts addObject:@{@"text": candidate.string, @"confidence": @(candidate.confidence)}];
            }
            [candidates addObject:top];
            [described addObject:@{@"quad": SWQuadJSON(observation), @"candidates": texts}];
        }
        *text = (void *)CFBridgingRetain(candidates);
        *lines = SWJSONString(described);
        return NULL;
    }
}

char *swipt_text_box(void *text, int64_t line, int64_t candidate, int64_t start, int64_t end, double quad[8], bool *found) {
    @autoreleasepool {
        NSArray<NSArray<VNRecognizedText *> *> *candidates = (__bridge NSArray *)text;
        *found = false;
        if (line < 0 || line >= (int64_t)candidates.count || candidate < 0 || candidate >= (int64_t)candidates[line].count) {
            return NULL;
        }
        VNRecognizedText *recognized = candidates[line][candidate];
        if (start < 0 || start > end || end > (int64_t)recognized.string.length) {
            return NULL;
        }
        NSError *failure = nil;
        VNRectangleObservation *box = [recognized boundingBoxForRange:NSMakeRange((NSUInteger)start, (NSUInteger)(end - start)) error:&failure];
        if (failure != nil) {
            return SWFailureCause(@"graphics: measure the text", failure);
        }
        if (box == nil) {
            return NULL;
        }
        CGPoint corners[4] = {box.topLeft, box.topRight, box.bottomRight, box.bottomLeft};
        for (int index = 0; index < 4; index++) {
            quad[index * 2] = corners[index].x;
            quad[index * 2 + 1] = corners[index].y;
        }
        *found = true;
        return NULL;
    }
}

char *swipt_barcodes_detect(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, void *token, char **codes) {
    @autoreleasepool {
        VNDetectBarcodesRequest *request = [VNDetectBarcodesRequest new];
        char *message = SWPerform(request, pixels, width, height, row_bytes, channels, token);
        if (message != NULL) {
            return message;
        }
        NSMutableArray *described = [NSMutableArray array];
        NSString *prefix = @"VNBarcodeSymbology";
        for (VNBarcodeObservation *observation in request.results) {
            NSString *symbology = observation.symbology;
            [described addObject:@{
                @"quad": SWQuadJSON(observation),
                @"payload": observation.payloadStringValue ?: NSNull.null,
                @"symbology": [symbology hasPrefix:prefix] ? [symbology substringFromIndex:prefix.length] : symbology,
            }];
        }
        *codes = SWJSONString(described);
        return NULL;
    }
}

char *swipt_feature_print(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, void *token, void **print) {
    @autoreleasepool {
        VNGenerateImageFeaturePrintRequest *request = [VNGenerateImageFeaturePrintRequest new];
        char *message = SWPerform(request, pixels, width, height, row_bytes, channels, token);
        if (message != NULL) {
            return message;
        }
        VNFeaturePrintObservation *observation = request.results.firstObject;
        if (observation == nil) {
            return SWFailure(@"graphics: Vision made no feature print");
        }
        *print = (void *)CFBridgingRetain(observation);
        return NULL;
    }
}

char *swipt_feature_distance(void *first, void *second, double *distance) {
    @autoreleasepool {
        float value = 0;
        NSError *failure = nil;
        if (![(__bridge VNFeaturePrintObservation *)first computeDistance:&value toFeaturePrintObservation:(__bridge VNFeaturePrintObservation *)second error:&failure]) {
            return SWFailureCause(@"graphics: compare the feature prints", failure);
        }
        *distance = value;
        return NULL;
    }
}
