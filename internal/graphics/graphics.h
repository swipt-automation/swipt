#ifndef SWIPT_GRAPHICS_H
#define SWIPT_GRAPHICS_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

enum {
    SWIPT_GRAPHICS_NONE = 0,
    SWIPT_GRAPHICS_BYTES = 1,
    SWIPT_GRAPHICS_HANDLE = 2,
    SWIPT_GRAPHICS_IMAGE = 3,
    SWIPT_GRAPHICS_RECORDING = 4,
    SWIPT_GRAPHICS_TEXT = 5,
};

enum {
    SWIPT_GRAPHICS_CROP = 1,
    SWIPT_GRAPHICS_RESIZE = 2,
    SWIPT_GRAPHICS_ROTATE = 3,
    SWIPT_GRAPHICS_GRAYSCALE = 4,
    SWIPT_GRAPHICS_THRESHOLD = 5,
    SWIPT_GRAPHICS_MASK = 6,
    SWIPT_GRAPHICS_INVERT = 7,
    SWIPT_GRAPHICS_DILATE = 8,
    SWIPT_GRAPHICS_ERODE = 9,
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
} swipt_graphics_taken;

typedef struct {
    double x;
    double y;
    double width;
    double height;
    double scale;
    double time;
    double timestamp;
    int64_t pixel_width;
    int64_t pixel_height;
    size_t cost;
} swipt_graphics_image;

void swipt_graphics_setup(void *lock, void *unlock, void *report_cost);
void swipt_graphics_install(void *context, int32_t command, void *value, uint64_t engine);

#endif
