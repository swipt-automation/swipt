//go:build darwin && cgo

#import "SWArguments.h"
#import "SWCall.h"
#import "SWCancelToken.h"
#import "SWError.h"
#import "SWGoBridge.h"
#import "SWImageNode.h"
#import "SWJSON.h"
#import "SWRaster.h"
#import "SWRegions.h"
#import "SWTemplateMatching.h"

static const double SWDefaultMatchThreshold = 0.8;

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
    return node == nil ? nil : @{@"image": @([call.context registerObject:node])};
}

id SWCallDecode(SWCall *call, NSError **error) {
    SWArguments *arguments = call.arguments;
    NSNumber *scale = [arguments number:@"scale"];
    NSData *data = nil;
    NSString *path = [arguments string:@"path"];
    if ([call.objects.firstObject isKindOfClass:NSData.class]) {
        data = call.objects.firstObject;
    } else if (path != nil) {
        data = [call.context readRoot:[arguments string:@"root"] path:path error:error];
        if (data == nil) {
            return nil;
        }
        if (scale == nil) {
            NSString *name = path.lastPathComponent;
            NSRegularExpression *expression = [NSRegularExpression regularExpressionWithPattern:@"@(\\d+(?:\\.\\d+)?)x\\.[^.]+$" options:0 error:nil];
            NSTextCheckingResult *match = [expression firstMatchInString:name options:0 range:NSMakeRange(0, name.length)];
            if (match != nil) {
                scale = @([name substringWithRange:[match rangeAtIndex:1]].doubleValue);
            }
        }
    } else {
        SWFail(error, SWErrorNameType, @"decode needs bytes or a file");
        return nil;
    }
    SWImageNode *node = [SWImageNode nodeWithEncoded:data scale:scale timestamp:NAN error:error];
    return node == nil ? nil : @{@"image": @([call.context registerObject:node])};
}

static NSData *SWEncodeFirstImage(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    SWRaster *raster = node == nil ? nil : [node raster:call.token error:error];
    if (raster == nil) {
        return nil;
    }
    return SWRasterEncode(raster, [call.arguments string:@"format"] ?: @"png", [call.arguments number:@"quality"], node.scale, error);
}

id SWCallEncode(SWCall *call, NSError **error) {
    NSData *data = SWEncodeFirstImage(call, error);
    return data == nil ? nil : @{@"bytes": @([call.context registerObject:data])};
}

id SWCallSave(SWCall *call, NSError **error) {
    NSData *data = SWEncodeFirstImage(call, error);
    if (data == nil) {
        return nil;
    }
    return [call.context writeData:data root:[call.arguments string:@"root"] path:[call.arguments string:@"path"] error:error] ? @{} : nil;
}

id SWCallPixel(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    if (node == nil) {
        return nil;
    }
    CGPoint point;
    if (![call.arguments point:@"point" into:&point]) {
        SWFail(error, SWErrorNameType, @"pixel needs a point");
        return nil;
    }
    CGPoint location = [node pixelPointInSpace:point];
    NSInteger x = (NSInteger)floor(location.x);
    NSInteger y = (NSInteger)floor(location.y);
    if (x < 0 || y < 0 || x >= node.pixelWidth || y >= node.pixelHeight) {
        SWFail(error, SWErrorNameRange, @"the point lies outside the image");
        return nil;
    }
    SWRaster *raster = [node raster:call.token error:error];
    if (raster == nil) {
        return nil;
    }
    NSInteger pixel[4];
    [raster readPixelAtX:x y:y into:pixel];
    return @[@(pixel[0]), @(pixel[1]), @(pixel[2]), @(pixel[3])];
}

id SWCallStats(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    if (node == nil) {
        return nil;
    }
    CGRect region;
    BOOL hasRegion = [call.arguments rect:@"region" into:&region];
    SWRaster *raster = [node rasterInSpace:hasRegion ? &region : NULL origin:NULL token:call.token error:error];
    return raster == nil ? nil : SWRasterStatistics(raster, error);
}

id SWCallFind(SWCall *call, NSError **error) {
    SWArguments *arguments = call.arguments;
    SWImageNode *node = [call image:0 error:error];
    SWImageNode *template = node == nil ? nil : [call image:1 error:error];
    if (template == nil) {
        return nil;
    }
    CGRect region;
    BOOL hasRegion = [arguments rect:@"region" into:&region];
    CGPoint origin = CGPointZero;
    SWRaster *target = [node rasterInSpace:hasRegion ? &region : NULL origin:&origin token:call.token error:error];
    SWRaster *pattern = target == nil ? nil : [template raster:call.token error:error];
    if (pattern == nil) {
        return nil;
    }
    double base = node.scale / template.scale;
    NSMutableArray<NSNumber *> *factors = [NSMutableArray array];
    for (NSNumber *scale in [arguments numbers:@"scales"] ?: @[@1]) {
        [factors addObject:@(scale.doubleValue * base)];
    }
    NSArray<SWMatch *> *matches = SWTemplateMatchingFind(target, pattern, factors, [arguments double:@"threshold" fallback:SWDefaultMatchThreshold],
                                                         [arguments integer:@"limit" fallback:100], [arguments double:@"overlap" fallback:0.3], call.token, error);
    if (matches == nil) {
        return nil;
    }
    NSMutableArray *described = [NSMutableArray array];
    for (SWMatch *match in matches) {
        CGRect frame = [node spaceRect:CGRectOffset(match.rect, origin.x, origin.y)];
        [described addObject:@{@"frame": SWRectJSON(frame), @"center": SWCenterJSON(frame), @"score": @(round(match.score * 1000) / 1000), @"scale": @(SWRounded(match.scale / base))}];
    }
    return described;
}

static BOOL SWCheckColor(NSDictionary *check, SWImageNode *node, SWRaster *raster) {
    NSArray<NSNumber *> *coordinates = check[@"point"];
    NSArray<NSNumber *> *color = check[@"color"];
    if (![coordinates isKindOfClass:NSArray.class] || coordinates.count != 2 || ![color isKindOfClass:NSArray.class] || color.count != 3) {
        return NO;
    }
    CGPoint location = [node pixelPointInSpace:CGPointMake(coordinates[0].doubleValue, coordinates[1].doubleValue)];
    NSInteger x = (NSInteger)floor(location.x);
    NSInteger y = (NSInteger)floor(location.y);
    if (x < 0 || y < 0 || x >= raster.width || y >= raster.height) {
        return NO;
    }
    NSNumber *tolerance = SWAsNumber(check[@"tolerance"]);
    NSInteger allowed = tolerance == nil ? 16 : tolerance.integerValue;
    NSInteger pixel[4];
    [raster readPixelAtX:x y:y into:pixel];
    for (NSInteger channel = 0; channel < 3; channel++) {
        if (labs(pixel[channel] - color[channel].integerValue) > allowed) {
            return NO;
        }
    }
    return YES;
}

id SWCallColors(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    SWRaster *raster = node == nil ? nil : [node raster:call.token error:error];
    if (raster == nil) {
        return nil;
    }
    NSMutableArray *results = [NSMutableArray array];
    for (NSDictionary *check in [call.arguments objects:@"checks"]) {
        [results addObject:SWCheckColor(check, node, raster) ? @YES : @NO];
    }
    return results;
}

id SWCallComponents(SWCall *call, NSError **error) {
    SWImageNode *node = [call image:0 error:error];
    SWRaster *raster = node == nil ? nil : [node raster:call.token error:error];
    SWRaster *binary = raster == nil ? nil : SWRegionsBinarized(raster, error);
    NSData *components = binary == nil ? nil : SWRegionsComponents(binary, [call.arguments integer:@"minimumPixels" fallback:1], call.token, error);
    if (components == nil) {
        return nil;
    }
    const SWComponent *items = components.bytes;
    NSUInteger count = components.length / sizeof(SWComponent);
    NSMutableArray *described = [NSMutableArray array];
    for (NSUInteger index = 0; index < count; index++) {
        [described addObject:@{
            @"frame": SWRectJSON([node spaceRect:SWComponentRect(items[index])]),
            @"pixels": @(items[index].count),
            @"centroid": [node spacePoint:SWComponentCentroid(items[index])],
        }];
    }
    return described;
}
