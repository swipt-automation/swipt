//go:build darwin && cgo

#import "SWRecognition.h"

#import <CoreML/CoreML.h>

#import "SWArguments.h"
#import "SWCancelToken.h"
#import "SWError.h"
#import "SWGoBridge.h"
#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWRaster.h"

static const double SWGlyphPixels = 24;

static const uint64_t SWCancelPollInterval = 20 * NSEC_PER_MSEC;

typedef CGPoint (^SWPlacePoint)(CGPoint point);

static NSDictionary<NSString *, id> *SWShape(VNRectangleObservation *observation, SWPlacePoint place) {
    CGPoint corners[4] = {place(observation.topLeft), place(observation.topRight), place(observation.bottomRight), place(observation.bottomLeft)};
    CGFloat minX = corners[0].x, maxX = corners[0].x, minY = corners[0].y, maxY = corners[0].y;
    NSMutableArray *quad = [NSMutableArray array];
    for (NSInteger index = 0; index < 4; index++) {
        minX = MIN(minX, corners[index].x);
        maxX = MAX(maxX, corners[index].x);
        minY = MIN(minY, corners[index].y);
        maxY = MAX(maxY, corners[index].y);
        [quad addObject:SWPointJSON(corners[index])];
    }
    CGRect frame = CGRectMake(minX, minY, maxX - minX, maxY - minY);
    return @{@"quad": quad, @"frame": SWRectJSON(frame), @"center": SWCenterJSON(frame)};
}

@implementation SWRecognizedText {
    NSArray<NSArray<VNRecognizedText *> *> *_candidates;
    SWPlacePoint _place;
}

- (instancetype)initWithObservations:(NSArray<VNRecognizedTextObservation *> *)observations place:(SWPlacePoint)place {
    self = [super init];
    if (self) {
        NSMutableArray *candidates = [NSMutableArray array];
        NSMutableArray *lines = [NSMutableArray array];
        for (VNRecognizedTextObservation *observation in observations) {
            NSArray<VNRecognizedText *> *top = [observation topCandidates:10];
            if (top.count == 0) {
                continue;
            }
            NSMutableArray *described = [NSMutableArray array];
            for (VNRecognizedText *candidate in top) {
                [described addObject:@{@"text": candidate.string, @"confidence": @(SWRounded(candidate.confidence))}];
            }
            NSMutableDictionary *line = [SWShape(observation, place) mutableCopy];
            line[@"text"] = top[0].string;
            line[@"confidence"] = @(SWRounded(top[0].confidence));
            line[@"candidates"] = described;
            [candidates addObject:top];
            [lines addObject:line];
        }
        _candidates = candidates;
        _lines = lines;
        _place = [place copy];
    }
    return self;
}

- (NSDictionary<NSString *, id> *)boundingBoxLine:(NSInteger)line candidate:(NSInteger)candidate start:(NSInteger)start end:(NSInteger)end error:(NSError **)error {
    if (line < 0 || line >= (NSInteger)_candidates.count || candidate < 0 || candidate >= (NSInteger)_candidates[line].count) {
        return nil;
    }
    VNRecognizedText *recognized = _candidates[line][candidate];
    if (start < 0 || start > end || end > (NSInteger)recognized.string.length) {
        return nil;
    }
    VNRectangleObservation *box = [recognized boundingBoxForRange:NSMakeRange(start, end - start) error:error];
    return box == nil ? nil : SWShape(box, _place);
}

@end

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

static BOOL SWPerform(VNRequest *request, SWRaster *raster, SWCancelToken *token, NSError **error) {
    CGImageRef image = SWRasterCreateImage(raster, error);
    if (image == NULL) {
        return NO;
    }
    VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithCGImage:image options:@{}];
    dispatch_source_t watch = nil;
    if (token != nil) {
        watch = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_global_queue(QOS_CLASS_UTILITY, 0));
        dispatch_source_set_timer(watch, DISPATCH_TIME_NOW, SWCancelPollInterval, SWCancelPollInterval / 4);
        dispatch_source_set_event_handler(watch, ^{
            if (token.isCancelled) {
                [request cancel];
            }
        });
        dispatch_resume(watch);
    }
    NSError *failure = nil;
    BOOL performed = [handler performRequests:@[request] error:&failure];
    if (watch != nil) {
        dispatch_source_cancel(watch);
    }
    CGImageRelease(image);
    if (!performed && ![token failIfCancelled:error]) {
        SWFailCause(error, SWErrorNameGeneric, failure, @"graphics: run %@", NSStringFromClass(request.class));
    }
    return performed;
}

void SWRecognitionWarm(void) {
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        VNRecognizeTextRequest *request = [VNRecognizeTextRequest new];
        request.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
        NSError *failure = nil;
        NSArray<NSString *> *languages = SWDeviceLanguages(request, &failure);
        if (languages != nil) {
            request.recognitionLanguages = languages;
        }
        if (languages == nil || !SWRestrict(request, &failure) || !SWPerform(request, [[SWRaster alloc] initWithWidth:64 height:32 channels:4], nil, &failure)) {
            SWLogError([NSString stringWithFormat:@"warm the text recognizer: %@", SWErrorMessageOf(failure)]);
        }
    });
}

SWRecognizedText *SWRecognizeText(SWImageNode *node, SWArguments *arguments, SWCancelToken *token, NSError **error) {
    CGRect region;
    BOOL hasRegion = [arguments rect:@"region" into:&region];
    CGPoint origin = CGPointZero;
    SWRaster *raster = [node rasterInSpace:hasRegion ? &region : NULL origin:&origin token:token error:error];
    if (raster == nil || [token failIfCancelled:error]) {
        return nil;
    }
    VNRecognizeTextRequest *request = [VNRecognizeTextRequest new];
    NSString *level = [arguments string:@"level"];
    if (level != nil) {
        request.recognitionLevel = [level isEqualToString:@"fast"] ? VNRequestTextRecognitionLevelFast : VNRequestTextRecognitionLevelAccurate;
    }
    NSNumber *correction = [arguments number:@"correction"];
    if (correction != nil) {
        request.usesLanguageCorrection = correction.boolValue;
    }
    NSArray<NSString *> *languages = [arguments strings:@"languages"] ?: SWDeviceLanguages(request, error);
    if (languages == nil) {
        return nil;
    }
    request.recognitionLanguages = languages;
    NSNumber *detect = [arguments number:@"detectLanguage"];
    if (detect != nil) {
        if (@available(iOS 16.0, macOS 13.0, *)) {
            request.automaticallyDetectsLanguage = detect.boolValue;
        }
    }
    NSArray<NSString *> *words = [arguments strings:@"words"];
    if (words != nil) {
        request.customWords = words;
    }
    SWRaster *sampled = raster;
    NSNumber *minimumHeight = [arguments number:@"minimumHeight"];
    if (minimumHeight != nil) {
        double height = minimumHeight.doubleValue;
        request.minimumTextHeight = (float)MIN(MAX(height * node.scale / raster.height, 0), 1);
        double density = SWGlyphPixels / height;
        if (density < node.scale) {
            sampled = SWRasterResized(raster, MAX((NSInteger)(raster.width * density / node.scale), 1), MAX((NSInteger)(raster.height * density / node.scale), 1), error);
            if (sampled == nil) {
                return nil;
            }
        }
    }
    if (!SWRestrict(request, error) || !SWPerform(request, sampled, token, error) || [token failIfCancelled:error]) {
        return nil;
    }
    double width = raster.width;
    double height = raster.height;
    CGAffineTransform transform = node.transform;
    return [[SWRecognizedText alloc] initWithObservations:request.results ?: @[] place:^CGPoint(CGPoint point) {
        return CGPointApplyAffineTransform(CGPointMake(origin.x + point.x * width, origin.y + (1 - point.y) * height), transform);
    }];
}

VNFeaturePrintObservation *SWFeaturePrint(SWImageNode *node, SWCancelToken *token, NSError **error) {
    VNGenerateImageFeaturePrintRequest *request = [VNGenerateImageFeaturePrintRequest new];
    SWRaster *raster = [node raster:token error:error];
    if (raster == nil || !SWRestrict(request, error) || !SWPerform(request, raster, token, error)) {
        return nil;
    }
    VNFeaturePrintObservation *observation = request.results.firstObject;
    if (observation == nil) {
        SWFail(error, SWErrorNameGeneric, @"graphics: Vision made no feature print");
    }
    return observation;
}

BOOL SWFeatureDistance(VNFeaturePrintObservation *reference, SWImageNode *other, double *distance, SWCancelToken *token, NSError **error) {
    VNFeaturePrintObservation *print = SWFeaturePrint(other, token, error);
    if (print == nil) {
        return NO;
    }
    float value = 0;
    if (![reference computeDistance:&value toFeaturePrintObservation:print error:error]) {
        return NO;
    }
    *distance = value;
    return YES;
}

NSArray<NSDictionary<NSString *, id> *> *SWDetectBarcodes(SWImageNode *node, SWCancelToken *token, NSError **error) {
    VNDetectBarcodesRequest *request = [VNDetectBarcodesRequest new];
    SWRaster *raster = [node raster:token error:error];
    if (raster == nil || !SWRestrict(request, error) || !SWPerform(request, raster, token, error)) {
        return nil;
    }
    double width = raster.width;
    double height = raster.height;
    CGAffineTransform transform = node.transform;
    SWPlacePoint place = ^CGPoint(CGPoint point) {
        return CGPointApplyAffineTransform(CGPointMake(point.x * width, (1 - point.y) * height), transform);
    };
    NSMutableArray *codes = [NSMutableArray array];
    NSString *prefix = @"VNBarcodeSymbology";
    for (VNBarcodeObservation *observation in request.results) {
        NSString *symbology = observation.symbology;
        NSMutableDictionary *code = [SWShape(observation, place) mutableCopy];
        code[@"payload"] = observation.payloadStringValue ?: NSNull.null;
        code[@"symbology"] = [symbology hasPrefix:prefix] ? [symbology substringFromIndex:prefix.length] : symbology;
        [codes addObject:code];
    }
    return codes;
}
