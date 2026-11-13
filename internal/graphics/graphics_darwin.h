#ifndef SWIPT_GRAPHICS_DARWIN_H
#define SWIPT_GRAPHICS_DARWIN_H

#include "graphics.h"

typedef struct {
    void *image;
    size_t length;
    double scale;
    char *failure;
} swipt_graphics_screenshot;

typedef struct {
    void *bytes;
    size_t length;
    char *failure;
} swipt_graphics_file;

typedef struct {
    const void *bytes;
    size_t length;
} swipt_graphics_bytes;

typedef struct {
    swipt_graphics_bytes sample;
    swipt_graphics_bytes vps;
    swipt_graphics_bytes sps;
    swipt_graphics_bytes pps;
    int64_t time;
    int32_t timescale;
    double arrival;
    int32_t turns;
    bool keyframe;
    bool damaged;
} swipt_graphics_unit;

typedef struct {
    const char *file;
    double start;
    int32_t turns;
} swipt_graphics_segment;

typedef struct {
    const char *info;
    double start;
    double duration;
    int64_t width;
    int64_t height;
    double scale;
    const swipt_graphics_segment *segments;
    size_t count;
} swipt_graphics_recording;

void swipt_graphics_warm(uintptr_t logger);
void swipt_graphics_install(void *context, int32_t command, void *value, uint64_t engine);
char *swipt_graphics_execute(const char *method, const char *arguments, void *const *objects, size_t count, uintptr_t scope);
void *swipt_graphics_decoder_create(uintptr_t stream, int64_t width, int64_t height, double scale);
bool swipt_graphics_decoder_decode(void *decoder, swipt_graphics_unit unit);
void swipt_graphics_decoder_close(void *decoder);
void *swipt_graphics_recording_open(swipt_graphics_recording recording);
char *swipt_graphics_recording_export(void *recording, const char *target, bool mp4, uintptr_t scope);

#endif
