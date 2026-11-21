//go:build android && cgo

#include <android/bitmap.h>
#include <android/data_space.h>
#include <android/imagedecoder.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "graphics_android.h"

bool swipt_image_inspect(const void *bytes, size_t length, int32_t *width, int32_t *height) {
    AImageDecoder *decoder = NULL;
    if (AImageDecoder_createFromBuffer(bytes, length, &decoder) != ANDROID_IMAGE_DECODER_SUCCESS) {
        return false;
    }
    const AImageDecoderHeaderInfo *info = AImageDecoder_getHeaderInfo(decoder);
    *width = AImageDecoderHeaderInfo_getWidth(info);
    *height = AImageDecoderHeaderInfo_getHeight(info);
    AImageDecoder_delete(decoder);
    return true;
}

static char *swipt_image_failure(const char *operation, int result) {
    const char *reason = AImageDecoder_resultToString(result);
    char *message = NULL;
    asprintf(&message, "graphics: %s: %s", operation, reason == NULL ? "unknown result" : reason);
    return message;
}

char *swipt_image_decode(const void *bytes, size_t length, uint8_t *pixels, int32_t width, int32_t height, size_t row_bytes) {
    AImageDecoder *decoder = NULL;
    int result = AImageDecoder_createFromBuffer(bytes, length, &decoder);
    if (result != ANDROID_IMAGE_DECODER_SUCCESS) {
        return swipt_image_failure("the image could not be decoded", result);
    }
    const char *operation = "set up the image decoder";
    result = AImageDecoder_setAndroidBitmapFormat(decoder, ANDROID_BITMAP_FORMAT_RGBA_8888);
    if (result == ANDROID_IMAGE_DECODER_SUCCESS) {
        result = AImageDecoder_setUnpremultipliedRequired(decoder, true);
    }
    if (result == ANDROID_IMAGE_DECODER_SUCCESS) {
        result = AImageDecoder_setDataSpace(decoder, ADATASPACE_SRGB);
    }
    if (result == ANDROID_IMAGE_DECODER_SUCCESS) {
        operation = "the image could not be decoded";
        result = AImageDecoder_decodeImage(decoder, pixels, row_bytes, row_bytes * (size_t)height);
    }
    AImageDecoder_delete(decoder);
    if (result != ANDROID_IMAGE_DECODER_SUCCESS) {
        return swipt_image_failure(operation, result);
    }
    for (int32_t row = 0; row < height; row++) {
        uint8_t *pixel = pixels + (size_t)row * row_bytes;
        for (int32_t column = 0; column < width; column++, pixel += 4) {
            uint8_t red = pixel[0];
            pixel[0] = pixel[2];
            pixel[2] = red;
        }
    }
    return NULL;
}
