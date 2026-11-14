//go:build cgo && !darwin

#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <JavaScriptCore/JSBasePrivate.h>
#include <JavaScriptCore/JavaScript.h>

#include "native_other.h"
#include "_cgo_export.h"

typedef void *(*swipt_lock_function)(JSContextRef context);
typedef void (*swipt_unlock_function)(void *lock);

static swipt_lock_function swipt_lock;
static swipt_unlock_function swipt_unlock;
static JSClassRef swipt_native_class;
static JSClassRef swipt_image_class;
static JSClassRef swipt_recording_class;
static JSClassRef swipt_resource_class;

static void swipt_set(JSContextRef context, JSObjectRef object, const char *name, JSValueRef value) {
    JSStringRef key = JSStringCreateWithUTF8CString(name);
    JSObjectSetProperty(context, object, key, value, kJSPropertyAttributeReadOnly | kJSPropertyAttributeDontDelete, NULL);
    JSStringRelease(key);
}

static JSValueRef swipt_string_value(JSContextRef context, const char *text) {
    JSStringRef string = JSStringCreateWithUTF8CString(text);
    JSValueRef value = JSValueMakeString(context, string);
    JSStringRelease(string);
    return value;
}

static char *swipt_string(JSContextRef context, JSValueRef value, JSValueRef *exception) {
    JSStringRef string = JSValueToStringCopy(context, value, exception);
    if (string == NULL) {
        return NULL;
    }
    size_t size = JSStringGetMaximumUTF8CStringSize(string);
    char *text = malloc(size);
    JSStringGetUTF8CString(string, text, size);
    JSStringRelease(string);
    return text;
}

static void swipt_throw(JSContextRef context, const char *name, const char *message, JSValueRef *exception) {
    bool native = strcmp(name, "TypeError") == 0 || strcmp(name, "RangeError") == 0;
    JSStringRef key = JSStringCreateWithUTF8CString(native ? name : "DOMException");
    JSValueRef constructor = JSObjectGetProperty(context, JSContextGetGlobalObject(context), key, NULL);
    JSStringRelease(key);
    JSValueRef arguments[2] = {swipt_string_value(context, message), swipt_string_value(context, name)};
    JSObjectRef error = NULL;
    if (constructor != NULL && JSValueIsObject(context, constructor) && JSObjectIsConstructor(context, (JSObjectRef)constructor)) {
        error = JSObjectCallAsConstructor(context, (JSObjectRef)constructor, native ? 1 : 2, arguments, NULL);
    }
    *exception = error != NULL ? error : JSObjectMakeError(context, 1, arguments, NULL);
}

static void swipt_free_bytes(void *bytes, void *context) {
    free(bytes);
}

static JSValueRef swipt_bytes_value(JSContextRef context, void *bytes, size_t length, JSValueRef *exception) {
    JSObjectRef array = JSObjectMakeTypedArrayWithBytesNoCopy(context, kJSTypedArrayTypeUint8Array, bytes, length, swipt_free_bytes, NULL, exception);
    return array != NULL ? array : JSValueMakeUndefined(context);
}

static uintptr_t swipt_handle_of(JSContextRef context, JSValueRef value) {
    if (JSValueIsObjectOfClass(context, value, swipt_image_class) || JSValueIsObjectOfClass(context, value, swipt_recording_class) ||
        JSValueIsObjectOfClass(context, value, swipt_resource_class)) {
        return (uintptr_t)JSObjectGetPrivate((JSObjectRef)value);
    }
    return 0;
}

static uintptr_t swipt_engine_of(JSContextRef context, JSObjectRef object) {
    if (!JSValueIsObjectOfClass(context, object, swipt_native_class)) {
        return 0;
    }
    return (uintptr_t)JSObjectGetPrivate(object);
}

static JSValueRef swipt_prepare(JSContextRef context, JSObjectRef function, JSObjectRef self, size_t count, const JSValueRef arguments[], JSValueRef *exception) {
    void *lock = swipt_lock(context);
    JSValueRef result = JSValueMakeUndefined(context);
    char *method = NULL;
    char *text = NULL;
    swipt_graphics_object *objects = NULL;
    size_t total = 0;
    swipt_graphics_failure failure = {0};
    int64_t identifier = 0;
    uintptr_t engine = swipt_engine_of(context, self);
    if (engine == 0 || count < 3 || !JSValueIsObject(context, arguments[2])) {
        swipt_throw(context, "TypeError", "graphics: prepare needs a method, its arguments and its objects", exception);
        goto done;
    }
    method = swipt_string(context, arguments[0], exception);
    text = method == NULL ? NULL : swipt_string(context, arguments[1], exception);
    if (text == NULL) {
        goto done;
    }
    JSStringRef key = JSStringCreateWithUTF8CString("length");
    double length = JSValueToNumber(context, JSObjectGetProperty(context, (JSObjectRef)arguments[2], key, NULL), exception);
    JSStringRelease(key);
    total = isfinite(length) && length > 0 ? (size_t)length : 0;
    objects = calloc(total > 0 ? total : 1, sizeof(swipt_graphics_object));
    for (size_t index = 0; index < total; index++) {
        JSValueRef item = JSObjectGetPropertyAtIndex(context, (JSObjectRef)arguments[2], (unsigned)index, exception);
        if (item == NULL) {
            goto done;
        }
        uintptr_t handle = swipt_handle_of(context, item);
        if (handle != 0) {
            objects[index].kind = SWIPT_GRAPHICS_HANDLE;
            objects[index].handle = handle;
            continue;
        }
        if (JSValueGetTypedArrayType(context, item, NULL) != kJSTypedArrayTypeUint8Array) {
            continue;
        }
        JSObjectRef array = (JSObjectRef)item;
        size_t size = JSObjectGetTypedArrayByteLength(context, array, NULL);
        // The pointer is to the start of the buffer rather than of the view
        // (oven-sh/WebKit d6375c72, Source/JavaScriptCore/API/JSTypedArray.cpp:248-251).
        uint8_t *buffer = size > 0 ? JSObjectGetTypedArrayBytesPtr(context, array, NULL) : NULL;
        if (size > 0 && buffer == NULL) {
            continue;
        }
        objects[index].kind = SWIPT_GRAPHICS_BYTES;
        objects[index].bytes = size > 0 ? buffer + JSObjectGetTypedArrayByteOffset(context, array, NULL) : NULL;
        objects[index].length = size;
    }
    identifier = swiptGraphicsPrepare(engine, method, text, objects, total, &failure);
    if (failure.name != NULL) {
        swipt_throw(context, failure.name, failure.message, exception);
        free(failure.name);
        free(failure.message);
    } else {
        result = JSValueMakeNumber(context, (double)identifier);
    }
done:
    free(method);
    free(text);
    free(objects);
    swipt_unlock(lock);
    return result;
}

static JSValueRef swipt_take(JSContextRef context, JSObjectRef function, JSObjectRef self, size_t count, const JSValueRef arguments[], JSValueRef *exception) {
    void *lock = swipt_lock(context);
    JSValueRef result = JSValueMakeNull(context);
    uintptr_t engine = swipt_engine_of(context, self);
    double identifier = count > 0 ? JSValueToNumber(context, arguments[0], NULL) : NAN;
    swipt_graphics_taken taken = {0};
    if (engine != 0 && isfinite(identifier)) {
        taken = swiptGraphicsTake(engine, (int64_t)identifier);
    }
    switch (taken.kind) {
    case SWIPT_GRAPHICS_IMAGE: {
        JSObjectRef image = JSObjectMake(context, swipt_image_class, (void *)taken.handle);
        swipt_graphics_image shown = swiptGraphicsImage(taken.handle);
        double width = shown.width;
        double height = shown.height;
        JSValueRef frame[4] = {JSValueMakeNumber(context, 0), JSValueMakeNumber(context, 0), JSValueMakeNumber(context, width), JSValueMakeNumber(context, height)};
        swipt_set(context, image, "width", JSValueMakeNumber(context, width));
        swipt_set(context, image, "height", JSValueMakeNumber(context, height));
        swipt_set(context, image, "scale", JSValueMakeNumber(context, shown.scale));
        swipt_set(context, image, "frame", JSObjectMakeArray(context, 4, frame, NULL));
        swipt_set(context, image, "time", JSValueMakeNumber(context, shown.time));
        swipt_set(context, image, "timestamp", JSValueMakeNumber(context, shown.timestamp));
        swipt_set(context, image, "pixelWidth", JSValueMakeNumber(context, (double)shown.pixel_width));
        swipt_set(context, image, "pixelHeight", JSValueMakeNumber(context, (double)shown.pixel_height));
        JSReportExtraMemoryCost(context, shown.cost);
        result = image;
        break;
    }
    case SWIPT_GRAPHICS_RECORDING: {
        JSObjectRef recording = JSObjectMake(context, swipt_recording_class, (void *)taken.handle);
        char *info = swiptGraphicsRecordingInfo(taken.handle);
        swipt_set(context, recording, "info", swipt_string_value(context, info));
        free(info);
        result = recording;
        break;
    }
    case SWIPT_GRAPHICS_HANDLE:
        result = JSObjectMake(context, swipt_resource_class, (void *)taken.handle);
        break;
    }
    swipt_unlock(lock);
    return result;
}

static JSValueRef swipt_bytes(JSContextRef context, JSObjectRef function, JSObjectRef self, size_t count, const JSValueRef arguments[], JSValueRef *exception) {
    void *lock = swipt_lock(context);
    JSValueRef result = JSValueMakeNull(context);
    uintptr_t engine = swipt_engine_of(context, self);
    double identifier = count > 0 ? JSValueToNumber(context, arguments[0], NULL) : NAN;
    size_t length = 0;
    void *bytes = engine == 0 || !isfinite(identifier) ? NULL : swiptGraphicsBytes(engine, (int64_t)identifier, &length);
    if (bytes != NULL) {
        result = swipt_bytes_value(context, bytes, length, exception);
    }
    swipt_unlock(lock);
    return result;
}

static JSValueRef swipt_show_bytes(JSContextRef context, JSObjectRef function, JSObjectRef self, size_t count, const JSValueRef arguments[], JSValueRef *exception) {
    void *lock = swipt_lock(context);
    JSValueRef result = JSValueMakeUndefined(context);
    size_t length = 0;
    void *bytes = JSValueIsObjectOfClass(context, self, swipt_image_class) ? swiptGraphicsShow((uintptr_t)JSObjectGetPrivate(self), &length) : NULL;
    if (bytes == NULL) {
        swipt_throw(context, "TypeError", "graphics: showBytes needs an Image", exception);
    } else {
        result = swipt_bytes_value(context, bytes, length, exception);
    }
    swipt_unlock(lock);
    return result;
}

static JSValueRef swipt_unsupported(JSContextRef context, const char *name, JSValueRef *exception) {
    void *lock = swipt_lock(context);
    char message[128];
    snprintf(message, sizeof(message), "graphics: %s is not available on this platform", name);
    swipt_throw(context, "NotSupportedError", message, exception);
    swipt_unlock(lock);
    return JSValueMakeUndefined(context);
}

#define SWIPT_UNSUPPORTED(name)                                                                                                                       \
    static JSValueRef swipt_unsupported_##name(JSContextRef context, JSObjectRef function, JSObjectRef self, size_t count, const JSValueRef arguments[], \
                                               JSValueRef *exception) {                                                                               \
        return swipt_unsupported(context, #name, exception);                                                                                          \
    }

SWIPT_UNSUPPORTED(crop)
SWIPT_UNSUPPORTED(resize)
SWIPT_UNSUPPORTED(rotate)
SWIPT_UNSUPPORTED(grayscale)
SWIPT_UNSUPPORTED(threshold)
SWIPT_UNSUPPORTED(mask)
SWIPT_UNSUPPORTED(invert)
SWIPT_UNSUPPORTED(dilate)
SWIPT_UNSUPPORTED(erode)
SWIPT_UNSUPPORTED(frame)

static void swipt_finalize(JSObjectRef object) {
    uintptr_t handle = (uintptr_t)JSObjectGetPrivate(object);
    if (handle != 0) {
        swiptGraphicsRelease(handle);
    }
}

static void swipt_finalize_native(JSObjectRef object) {
    uintptr_t engine = (uintptr_t)JSObjectGetPrivate(object);
    if (engine != 0) {
        swiptGraphicsDeleteHandle(engine);
    }
}

#define SWIPT_FUNCTION(name, function) {name, function, kJSPropertyAttributeReadOnly | kJSPropertyAttributeDontDelete | kJSPropertyAttributeDontEnum}

void swipt_graphics_setup(void *lock, void *unlock) {
    swipt_lock = (swipt_lock_function)lock;
    swipt_unlock = (swipt_unlock_function)unlock;
    static const JSStaticFunction native[] = {
        SWIPT_FUNCTION("prepare", swipt_prepare),
        SWIPT_FUNCTION("take", swipt_take),
        SWIPT_FUNCTION("bytes", swipt_bytes),
        {NULL, NULL, 0},
    };
    static const JSStaticFunction image[] = {
        SWIPT_FUNCTION("crop", swipt_unsupported_crop),
        SWIPT_FUNCTION("resize", swipt_unsupported_resize),
        SWIPT_FUNCTION("rotate", swipt_unsupported_rotate),
        SWIPT_FUNCTION("grayscale", swipt_unsupported_grayscale),
        SWIPT_FUNCTION("threshold", swipt_unsupported_threshold),
        SWIPT_FUNCTION("mask", swipt_unsupported_mask),
        SWIPT_FUNCTION("invert", swipt_unsupported_invert),
        SWIPT_FUNCTION("dilate", swipt_unsupported_dilate),
        SWIPT_FUNCTION("erode", swipt_unsupported_erode),
        SWIPT_FUNCTION("showBytes", swipt_show_bytes),
        {NULL, NULL, 0},
    };
    static const JSStaticFunction recording[] = {
        SWIPT_FUNCTION("frame", swipt_unsupported_frame),
        {NULL, NULL, 0},
    };
    JSClassDefinition definition = kJSClassDefinitionEmpty;
    definition.className = "GraphicsNative";
    definition.staticFunctions = native;
    definition.finalize = swipt_finalize_native;
    swipt_native_class = JSClassCreate(&definition);
    definition = kJSClassDefinitionEmpty;
    definition.className = "Image";
    definition.staticFunctions = image;
    definition.finalize = swipt_finalize;
    swipt_image_class = JSClassCreate(&definition);
    definition = kJSClassDefinitionEmpty;
    definition.className = "ScreenRecording";
    definition.staticFunctions = recording;
    definition.finalize = swipt_finalize;
    swipt_recording_class = JSClassCreate(&definition);
    definition = kJSClassDefinitionEmpty;
    definition.className = "GraphicsHandle";
    definition.finalize = swipt_finalize;
    swipt_resource_class = JSClassCreate(&definition);
}

void swipt_graphics_install(void *context, int32_t command, void *value, uint64_t engine) {
    JSStringRef key = JSStringCreateWithUTF8CString("graphics");
    JSObjectSetProperty((JSContextRef)context, (JSObjectRef)value, key, JSObjectMake((JSContextRef)context, swipt_native_class, (void *)(uintptr_t)engine), kJSPropertyAttributeNone, NULL);
    JSStringRelease(key);
}
