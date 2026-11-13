//go:build cgo && !darwin

#ifndef SWIPT_GRAPHICS_NATIVE_H
#define SWIPT_GRAPHICS_NATIVE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include <JavaScriptCore/JavaScript.h>

enum {
    SWIPT_GRAPHICS_UNKNOWN = 0,
    SWIPT_GRAPHICS_BYTES = 1,
    SWIPT_GRAPHICS_HANDLE = 2,
};

enum {
    SWIPT_GRAPHICS_NONE = 0,
    SWIPT_GRAPHICS_IMAGE = 1,
    SWIPT_GRAPHICS_RESOURCE = 2,
    SWIPT_GRAPHICS_RECORDING = 3,
};

typedef struct {
    int32_t kind;
    uintptr_t handle;
    const void *bytes;
    size_t length;
} swipt_graphics_object;

typedef struct {
    char *name;
    char *message;
} swipt_graphics_failure;

typedef struct {
    int32_t kind;
    uintptr_t handle;
    double width;
    double height;
    double scale;
    double time;
    double timestamp;
    int64_t pixel_width;
    int64_t pixel_height;
    size_t cost;
    char *info;
} swipt_graphics_taken;

void swipt_graphics_setup(void *lock, void *unlock);
void swipt_graphics_native(JSGlobalContextRef context, int32_t command, JSValueRef value, uint64_t engine);

#endif
