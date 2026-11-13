//go:build cgo && !darwin

#ifndef SWIPT_GRAPHICS_NATIVE_H
#define SWIPT_GRAPHICS_NATIVE_H

#include "graphics.h"

typedef struct {
    double width;
    double height;
    double scale;
    double time;
    double timestamp;
    int64_t pixel_width;
    int64_t pixel_height;
    size_t cost;
} swipt_graphics_image;

void swipt_graphics_setup(void *lock, void *unlock);
void swipt_graphics_install(void *context, int32_t command, void *value, uint64_t engine);

#endif
