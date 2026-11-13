#ifndef SWIPT_GRAPHICS_H
#define SWIPT_GRAPHICS_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

enum {
    SWIPT_GRAPHICS_NONE = 0,
    SWIPT_GRAPHICS_BYTES = 1,
    SWIPT_GRAPHICS_HANDLE = 2,
    SWIPT_GRAPHICS_NATIVE = 3,
    SWIPT_GRAPHICS_IMAGE = 4,
    SWIPT_GRAPHICS_RECORDING = 5,
};

typedef struct {
    int32_t kind;
    uintptr_t handle;
    void *native;
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
    void *native;
} swipt_graphics_taken;

#endif
