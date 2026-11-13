#ifndef SWIPT_GRAPHICS_H
#define SWIPT_GRAPHICS_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "screen.h"

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
    char *path;
    char *failure;
} swipt_graphics_directory;

typedef struct {
    uintptr_t file;
    char *path;
    char *failure;
} swipt_graphics_staged_file;

typedef struct {
    uintptr_t stream;
    void *frames;
    char *failure;
} swipt_graphics_screen_stream;

typedef struct {
    uintptr_t session;
    char *failure;
} swipt_graphics_recording_session;

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
    double arrival;
    int32_t turns;
    bool keyframe;
    bool damaged;
} swipt_graphics_unit;

void swipt_graphics_native(void *context, int32_t command, void *value, uint64_t engine);
void swipt_graphics_warm(void);
char *swipt_graphics_run(uint64_t engine, int64_t call, uintptr_t scope);
char *swipt_graphics_watch(uint64_t engine, int64_t call, uintptr_t scope);
void *swipt_graphics_frames_create(int64_t width, int64_t height, double scale);
void *swipt_graphics_frames_retain(void *frames);
bool swipt_graphics_frames_decode(void *frames, swipt_graphics_unit unit);
void swipt_graphics_frames_end(void *frames, const char *failure);
void swipt_graphics_frames_close(void *frames);

#endif
