//go:build darwin && cgo

#include <Accelerate/Accelerate.h>
#include <CoreGraphics/CoreGraphics.h>

#include "accelerate_darwin.h"

static vImage_Buffer swipt_buffer(const void *data, long width, long height, long row_bytes) {
    return (vImage_Buffer){.data = (void *)data, .width = (vImagePixelCount)width, .height = (vImagePixelCount)height, .rowBytes = (size_t)row_bytes};
}

long swipt_scale(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_width, long out_height, long out_row_bytes, int channels) {
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    vImage_Buffer destination = swipt_buffer(out, out_width, out_height, out_row_bytes);
    return channels == 1 ? vImageScale_Planar8(&source, &destination, NULL, kvImageHighQualityResampling)
                         : vImageScale_ARGB8888(&source, &destination, NULL, kvImageHighQualityResampling);
}

long swipt_turn(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, int channels, int quarters) {
    static const uint8_t constants[] = {kRotate0DegreesClockwise, kRotate90DegreesClockwise, kRotate180DegreesClockwise, kRotate270DegreesClockwise};
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    vImage_Buffer destination = quarters % 2 == 0 ? swipt_buffer(out, width, height, out_row_bytes) : swipt_buffer(out, height, width, out_row_bytes);
    if (channels == 1) {
        return vImageRotate90_Planar8(&source, &destination, constants[quarters], 0, kvImageNoFlags);
    }
    const Pixel_8888 back = {0, 0, 0, 0};
    return vImageRotate90_ARGB8888(&source, &destination, constants[quarters], back, kvImageNoFlags);
}

static vImage_CGImageFormat swipt_format(int channels) {
    static CGColorSpaceRef color;
    static CGColorSpaceRef gray;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        color = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        gray = CGColorSpaceCreateDeviceGray();
    });
    if (channels == 1) {
        return (vImage_CGImageFormat){.bitsPerComponent = 8, .bitsPerPixel = 8, .colorSpace = gray, .bitmapInfo = (CGBitmapInfo)kCGImageAlphaNone};
    }
    return (vImage_CGImageFormat){.bitsPerComponent = 8, .bitsPerPixel = 32, .colorSpace = color, .bitmapInfo = (CGBitmapInfo)kCGImageAlphaFirst | kCGBitmapByteOrder32Little};
}

CGImageRef swipt_image_create(const uint8_t *in, long width, long height, long row_bytes, int channels, long *status) {
    vImage_Buffer buffer = swipt_buffer(in, width, height, row_bytes);
    vImage_CGImageFormat format = swipt_format(channels);
    vImage_Error error = kvImageNoError;
    CGImageRef image = vImageCreateCGImageFromBuffer(&buffer, &format, NULL, NULL, kvImageNoFlags, &error);
    *status = error;
    return image;
}

long swipt_rotate(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_width, long out_height, long out_row_bytes, int channels, double degrees) {
    long status = kvImageNoError;
    CGImageRef image = swipt_image_create(in, width, height, row_bytes, channels, &status);
    if (image == NULL) {
        return status;
    }
    vImage_CGImageFormat format = swipt_format(channels);
    uint32_t info = channels == 1 ? (uint32_t)kCGImageAlphaNone : (uint32_t)kCGImageAlphaPremultipliedFirst | (uint32_t)kCGBitmapByteOrder32Little;
    CGContextRef context = CGBitmapContextCreate(out, (size_t)out_width, (size_t)out_height, 8, (size_t)out_row_bytes, format.colorSpace, info);
    if (context == NULL) {
        CGImageRelease(image);
        return kvImageInternalError;
    }
    CGContextSetInterpolationQuality(context, kCGInterpolationHigh);
    CGContextTranslateCTM(context, 0, out_height);
    CGContextScaleCTM(context, 1, -1);
    CGContextTranslateCTM(context, out_width / 2.0, out_height / 2.0);
    CGContextRotateCTM(context, degrees * M_PI / 180);
    CGContextTranslateCTM(context, -width / 2.0, -height / 2.0);
    CGContextTranslateCTM(context, 0, height);
    CGContextScaleCTM(context, 1, -1);
    CGContextDrawImage(context, CGRectMake(0, 0, width, height), image);
    CGContextRelease(context);
    CGImageRelease(image);
    if (channels == 1) {
        return kvImageNoError;
    }
    vImage_Buffer buffer = swipt_buffer(out, out_width, out_height, out_row_bytes);
    return vImageUnpremultiplyData_RGBA8888(&buffer, &buffer, kvImageNoFlags);
}

long swipt_mix(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, const int16_t weights[4], int32_t divisor) {
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    vImage_Buffer destination = swipt_buffer(out, width, height, out_row_bytes);
    return vImageMatrixMultiply_ARGB8888ToPlanar8(&source, &destination, weights, divisor, NULL, 0, kvImageNoFlags);
}

long swipt_map(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, const uint8_t table[256]) {
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    vImage_Buffer destination = swipt_buffer(out, width, height, out_row_bytes);
    return vImageTableLookUp_Planar8(&source, &destination, table, kvImageNoFlags);
}

long swipt_map_channels(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, const uint8_t tables[4][256]) {
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    vImage_Buffer destination = swipt_buffer(out, width, height, out_row_bytes);
    return vImageTableLookUp_ARGB8888(&source, &destination, tables[0], tables[1], tables[2], tables[3], kvImageNoFlags);
}

long swipt_histograms(const uint8_t *in, long width, long height, long row_bytes, int channels, uint64_t *counts) {
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    if (channels == 1) {
        return vImageHistogramCalculation_Planar8(&source, (vImagePixelCount *)counts, kvImageNoFlags);
    }
    vImagePixelCount *histograms[4] = {(vImagePixelCount *)counts, (vImagePixelCount *)counts + 256, (vImagePixelCount *)counts + 512, (vImagePixelCount *)counts + 768};
    return vImageHistogramCalculation_ARGB8888(&source, histograms, kvImageNoFlags);
}

void swipt_absolute_difference(const uint8_t *a, long a_row_bytes, const uint8_t *b, long b_row_bytes, long width, long height, uint8_t *out, long out_row_bytes) {
    for (long y = 0; y < height; y++) {
        const uint8_t *first = a + y * a_row_bytes;
        const uint8_t *second = b + y * b_row_bytes;
        uint8_t *target = out + y * out_row_bytes;
        for (long x = 0; x < width; x++) {
            target[x] = first[x] > second[x] ? first[x] - second[x] : second[x] - first[x];
        }
    }
}

long swipt_extreme(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, int channels, long size, bool maximum) {
    vImage_Buffer source = swipt_buffer(in, width, height, row_bytes);
    vImage_Buffer destination = swipt_buffer(out, width, height, out_row_bytes);
    vImagePixelCount side = (vImagePixelCount)size;
    if (channels == 1) {
        return maximum ? vImageMax_Planar8(&source, &destination, NULL, 0, 0, side, side, kvImageNoFlags)
                       : vImageMin_Planar8(&source, &destination, NULL, 0, 0, side, side, kvImageNoFlags);
    }
    return maximum ? vImageMax_ARGB8888(&source, &destination, NULL, 0, 0, side, side, kvImageNoFlags)
                   : vImageMin_ARGB8888(&source, &destination, NULL, 0, 0, side, side, kvImageNoFlags);
}

void swipt_plane_of(const uint8_t *in, long width, long height, long row_bytes, float *out) {
    float scale = 1.f / 255;
    for (long y = 0; y < height; y++) {
        vDSP_vfltu8(in + y * row_bytes, 1, out + y * width, 1, (vDSP_Length)width);
    }
    vDSP_vsmul(out, 1, &scale, out, 1, (vDSP_Length)(width * height));
}

long swipt_plane_scale(const float *in, long width, long height, float *out, long out_width, long out_height) {
    vImage_Buffer source = swipt_buffer(in, width, height, width * 4);
    vImage_Buffer destination = swipt_buffer(out, out_width, out_height, out_width * 4);
    return vImageScale_PlanarF(&source, &destination, NULL, kvImageHighQualityResampling);
}

void swipt_plane_square(const float *in, float *out, long count) {
    vDSP_vsq(in, 1, out, 1, (vDSP_Length)count);
}

float swipt_sum(const float *values, long count) {
    float total = 0;
    vDSP_sve(values, 1, &total, (vDSP_Length)count);
    return total;
}

float swipt_dot(const float *a, const float *b, long count) {
    float total = 0;
    vDSP_dotpr(a, 1, b, 1, &total, (vDSP_Length)count);
    return total;
}

void swipt_centered_product(const float *values, const float *weights, float mean, float *out, long count) {
    float negative = -mean;
    vDSP_vsadd(values, 1, &negative, out, 1, (vDSP_Length)count);
    vDSP_vmul(out, 1, weights, 1, out, 1, (vDSP_Length)count);
}

void swipt_correlate(const float *image, long width, long height, const float *kernel, long kernel_width, long kernel_height, float *out) {
    long rows = height - kernel_height + 1;
    long columns = width - kernel_width + 1;
    float *total = calloc((size_t)(rows * width), sizeof(float));
    float *part = calloc((size_t)(rows * width), sizeof(float));
    vDSP_Length length = (vDSP_Length)(rows * width - (kernel_width - 1));
    for (long line = 0; line < kernel_height; line++) {
        vDSP_conv(image + line * width, 1, kernel + line * kernel_width, 1, part, 1, length, (vDSP_Length)kernel_width);
        vDSP_vadd(total, 1, part, 1, total, 1, length);
    }
    for (long row = 0; row < rows; row++) {
        memcpy(out + row * columns, total + row * width, (size_t)columns * sizeof(float));
    }
    free(part);
    free(total);
}

static float swipt_window_sum(const float *image, long width, long x, long y, const float *kernel, long kernel_width, long kernel_height) {
    float sum = 0;
    for (long line = 0; line < kernel_height; line++) {
        float part = 0;
        vDSP_dotpr(image + (y + line) * width + x, 1, kernel + line * kernel_width, 1, &part, (vDSP_Length)kernel_width);
        sum += part;
    }
    return sum;
}

void swipt_window_sums(const float *image, const float *squares, long width, const float *centered, const float *weights, long kernel_width, long kernel_height,
                       const int64_t *origins, long count, float *out) {
    for (long index = 0; index < count; index++) {
        long x = (long)origins[index * 2];
        long y = (long)origins[index * 2 + 1];
        out[index * 3] = swipt_window_sum(image, width, x, y, centered, kernel_width, kernel_height);
        out[index * 3 + 1] = swipt_window_sum(image, width, x, y, weights, kernel_width, kernel_height);
        out[index * 3 + 2] = swipt_window_sum(squares, width, x, y, weights, kernel_width, kernel_height);
    }
}

void swipt_color_window_sums(const uint8_t *target, long row_bytes, long x, long y, const float *first, const float *second, const float *third, const float *weights, long width,
                             long height, float *out) {
    const float *centered[3] = {first, second, third};
    float scale = 1.f / 255;
    vDSP_Length count = (vDSP_Length)width;
    float *values = malloc((size_t)width * sizeof(float));
    for (long channel = 0; channel < 3; channel++) {
        float correlation = 0;
        float sum = 0;
        float square = 0;
        for (long line = 0; line < height; line++) {
            vDSP_vfltu8(target + (y + line) * row_bytes + x * 4 + channel, 4, values, 1, count);
            vDSP_vsmul(values, 1, &scale, values, 1, count);
            float part = 0;
            vDSP_dotpr(values, 1, centered[channel] + line * width, 1, &part, count);
            correlation += part;
            vDSP_dotpr(values, 1, weights + line * width, 1, &part, count);
            sum += part;
            vDSP_vsq(values, 1, values, 1, count);
            vDSP_dotpr(values, 1, weights + line * width, 1, &part, count);
            square += part;
        }
        out[channel * 3] = correlation;
        out[channel * 3 + 1] = sum;
        out[channel * 3 + 2] = square;
    }
    free(values);
}
