#ifndef SWIPT_GRAPHICS_H
#define SWIPT_GRAPHICS_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

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
    int64_t width;
    int64_t height;
    double scale;
    char *failure;
} swipt_graphics_screen_stream;

void swipt_graphics_native(void *context, int32_t command, void *value, uint64_t engine);
void swipt_graphics_warm(void);
char *swipt_graphics_run(uint64_t engine, int64_t call, uintptr_t scope);
char *swipt_graphics_watch(uint64_t engine, int64_t call, uintptr_t scope);
void swipt_graphics_screen_stream_packets(const void *bytes, size_t length);
void swipt_graphics_screen_stream_ended(const char *failure);

#endif
