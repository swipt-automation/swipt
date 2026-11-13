//go:build darwin && cgo

#import "SWArguments.h"
#import "SWCall.h"
#import "SWError.h"
#import "SWGoBridge.h"
#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWRaster.h"
#import "SWRecognition.h"
#import "SWRegions.h"

id SWCallText(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    SWRecognizedText *text = node == nil ? nil : SWRecognizeText(node, call.arguments, call.token, error);
    return text == nil ? nil : @{@"lines": text.lines, @"text": @([call.context registerObject:text])};
}

id SWCallDifference(SWImageNode *first, SWImageNode *second, NSInteger threshold, SWCancelToken *token, NSError **error) {
    SWRaster *a = [first raster:token error:error];
    SWRaster *other = a == nil ? nil : [second raster:token error:error];
    SWRaster *b = other == nil ? nil : SWRasterResized(other, a.width, a.height, error);
    if (b == nil) {
        return nil;
    }
    NSInteger changed = 0;
    SWRaster *mask = SWRegionsDifference(a, b, threshold, &changed, error);
    NSData *regions = mask == nil ? nil : SWRegionsMerged(mask, MAX(2, a.width / 200), token, error);
    if (regions == nil) {
        return nil;
    }
    const CGRect *rects = regions.bytes;
    NSUInteger count = regions.length / sizeof(CGRect);
    NSMutableArray *frames = [NSMutableArray array];
    for (NSUInteger index = 0; index < count; index++) {
        [frames addObject:SWRectJSON([first spaceRect:rects[index]])];
    }
    return @{@"ratio": @(round((double)changed / MAX(a.width * a.height, 1) * 10000) / 10000), @"frames": frames};
}

id SWCallDiff(SWCall *call, NSError **error) {
    SWImageNode *first = [call image:0 error:error];
    SWImageNode *second = first == nil ? nil : [call image:1 error:error];
    if (second == nil) {
        return nil;
    }
    return SWCallDifference(first, second, [call.arguments integer:@"threshold" fallback:SWDefaultDifferenceThreshold], call.token, error);
}

id SWCallDistance(SWCall *call, NSError **error) {
    SWImageNode *first = [call image:0 error:error];
    VNFeaturePrintObservation *reference = first == nil ? nil : SWFeaturePrint(first, call.token, error);
    SWImageNode *second = reference == nil ? nil : [call image:1 error:error];
    double distance = 0;
    if (second == nil || !SWFeatureDistance(reference, second, &distance, call.token, error)) {
        return nil;
    }
    return @(SWRounded(distance * 1000) / 1000);
}

id SWCallNearest(SWCall *call, NSError **error) {
    SWImageNode *first = [call image:0 error:error];
    VNFeaturePrintObservation *reference = first == nil ? nil : SWFeaturePrint(first, call.token, error);
    if (reference == nil) {
        return nil;
    }
    NSInteger bestIndex = -1;
    double bestDistance = 0;
    for (NSUInteger index = 1; index < call.objects.count; index++) {
        SWImageNode *candidate = [call image:index error:error];
        double distance = 0;
        if (candidate == nil || !SWFeatureDistance(reference, candidate, &distance, call.token, error)) {
            return nil;
        }
        if (bestIndex < 0 || distance < bestDistance) {
            bestIndex = (NSInteger)index - 1;
            bestDistance = distance;
        }
    }
    if (bestIndex < 0) {
        SWFail(error, SWErrorNameType, @"nearest needs candidates");
        return nil;
    }
    return @{@"index": @(bestIndex), @"distance": @(SWRounded(bestDistance * 1000) / 1000)};
}

id SWCallBarcodes(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    return node == nil ? nil : SWDetectBarcodes(node, call.token, error);
}
