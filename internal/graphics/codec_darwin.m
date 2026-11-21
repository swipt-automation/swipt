//go:build darwin && cgo

#import <Accelerate/Accelerate.h>
#import <ImageIO/ImageIO.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>

#include "accelerate_darwin.h"
#include "graphics_darwin.h"

#import "error_darwin.h"

static NSInteger SWIntegerOf(id value) {
    return [value isKindOfClass:NSNumber.class] ? ((NSNumber *)value).integerValue : 0;
}

static NSInteger SWOrientationOf(NSDictionary *properties) {
    NSInteger orientation = SWIntegerOf(properties[(__bridge id)kCGImagePropertyOrientation]);
    return orientation >= 1 && orientation <= 8 ? orientation : 1;
}

char *swipt_image_inspect(const void *bytes, size_t length, swipt_image_header *header) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:bytes length:length];
        CGImageSourceRef source = CGImageSourceCreateWithData((__bridge CFDataRef)data, NULL);
        NSDictionary *properties = source == NULL ? nil : CFBridgingRelease(CGImageSourceCopyPropertiesAtIndex(source, 0, NULL));
        if (source != NULL) {
            CFRelease(source);
        }
        NSInteger width = SWIntegerOf(properties[(__bridge id)kCGImagePropertyPixelWidth]);
        NSInteger height = SWIntegerOf(properties[(__bridge id)kCGImagePropertyPixelHeight]);
        if (width <= 0 || height <= 0) {
            return SWFailure(@"graphics: the bytes are not an image");
        }
        BOOL turned = SWOrientationOf(properties) >= 5;
        id dpi = properties[(__bridge id)kCGImagePropertyDPIWidth];
        *header = (swipt_image_header){
            .width = turned ? height : width,
            .height = turned ? width : height,
            .dpi = [dpi isKindOfClass:NSNumber.class] ? ((NSNumber *)dpi).doubleValue : 0,
        };
        return NULL;
    }
}

char *swipt_image_decode(const void *bytes, size_t length, uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:bytes length:length];
        CGImageSourceRef source = CGImageSourceCreateWithData((__bridge CFDataRef)data, NULL);
        if (source == NULL) {
            return SWFailure(@"graphics: the bytes are not an image");
        }
        NSDictionary *properties = CFBridgingRelease(CGImageSourceCopyPropertiesAtIndex(source, 0, NULL));
        CGImageRef image;
        if (SWOrientationOf(properties) == 1) {
            image = CGImageSourceCreateImageAtIndex(source, 0, (__bridge CFDictionaryRef)@{(__bridge id)kCGImageSourceShouldCacheImmediately: @YES});
        } else {
            image = CGImageSourceCreateThumbnailAtIndex(source, 0, (__bridge CFDictionaryRef)@{
                (__bridge id)kCGImageSourceCreateThumbnailFromImageAlways: @YES,
                (__bridge id)kCGImageSourceCreateThumbnailWithTransform: @YES,
                (__bridge id)kCGImageSourceThumbnailMaxPixelSize: @(MAX(width, height)),
                (__bridge id)kCGImageSourceShouldCacheImmediately: @YES,
            });
        }
        CGImageSourceStatus status = CGImageSourceGetStatusAtIndex(source, 0);
        CFRelease(source);
        if (image == NULL) {
            return SWFailure([NSString stringWithFormat:@"graphics: the image could not be decoded (CGImageSourceStatus %d)", (int)status]);
        }
        size_t decodedWidth = CGImageGetWidth(image);
        size_t decodedHeight = CGImageGetHeight(image);
        if ((int64_t)decodedWidth != width || (int64_t)decodedHeight != height) {
            CGImageRelease(image);
            return SWFailure([NSString stringWithFormat:@"graphics: the image decoded as %zux%zu pixels instead of %lldx%lld", decodedWidth, decodedHeight, width, height]);
        }
        static CGColorSpaceRef space;
        static dispatch_once_t once;
        dispatch_once(&once, ^{
            space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        });
        vImage_CGImageFormat format = {.bitsPerComponent = 8, .bitsPerPixel = 32, .colorSpace = space, .bitmapInfo = (CGBitmapInfo)kCGImageAlphaFirst | kCGBitmapByteOrder32Little};
        vImage_Buffer buffer = {.data = pixels, .width = (vImagePixelCount)width, .height = (vImagePixelCount)height, .rowBytes = (size_t)row_bytes};
        vImage_Error error = vImageBuffer_InitWithCGImage(&buffer, &format, NULL, image, kvImageNoAllocate);
        CGImageRelease(image);
        return error == kvImageNoError ? NULL : SWFailure([NSString stringWithFormat:@"graphics: read the image: vImage error %ld", (long)error]);
    }
}

char *swipt_image_encode(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, const char *format, double quality, double scale, swipt_encoded *encoded) {
    @autoreleasepool {
        NSString *name = [NSString stringWithUTF8String:format];
        UTType *type = [name isEqualToString:@"jpeg"] ? UTTypeJPEG : [name isEqualToString:@"heic"] ? UTTypeHEIC : UTTypePNG;
        long status = 0;
        CGImageRef image = swipt_image_create(pixels, (long)width, (long)height, (long)row_bytes, channels, &status);
        if (image == NULL) {
            return SWFailure([NSString stringWithFormat:@"graphics: create the image: vImage error %ld", status]);
        }
        NSMutableData *data = [NSMutableData data];
        CGImageDestinationRef destination = CGImageDestinationCreateWithData((__bridge CFMutableDataRef)data, (__bridge CFStringRef)type.identifier, 1, NULL);
        if (destination == NULL) {
            CGImageRelease(image);
            return SWFailure([NSString stringWithFormat:@"graphics: create the %@ encoder", name]);
        }
        NSMutableDictionary *properties = [@{(__bridge id)kCGImagePropertyDPIWidth: @(72 * scale), (__bridge id)kCGImagePropertyDPIHeight: @(72 * scale)} mutableCopy];
        if (quality >= 0) {
            properties[(__bridge id)kCGImageDestinationLossyCompressionQuality] = @(quality);
        }
        CGImageDestinationAddImage(destination, image, (__bridge CFDictionaryRef)properties);
        BOOL finished = CGImageDestinationFinalize(destination);
        CFRelease(destination);
        CGImageRelease(image);
        if (!finished) {
            return SWFailure([NSString stringWithFormat:@"graphics: encode the image as %@", name]);
        }
        encoded->data = (void *)CFBridgingRetain(data);
        encoded->bytes = (void *)data.bytes;
        encoded->length = data.length;
        return NULL;
    }
}
