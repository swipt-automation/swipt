#ifndef SWIPT_GRAPHICS_DARWIN_H
#define SWIPT_GRAPHICS_DARWIN_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    const void *bytes;
    size_t length;
} swipt_graphics_bytes;

typedef struct {
    int64_t width;
    int64_t height;
    double dpi;
} swipt_image_header;

typedef struct {
    void *data;
    const void *bytes;
    size_t length;
} swipt_encoded;

typedef struct {
    swipt_graphics_bytes sample;
    swipt_graphics_bytes vps;
    swipt_graphics_bytes sps;
    swipt_graphics_bytes pps;
    int64_t time;
    int32_t timescale;
    bool keyframe;
    bool damaged;
} swipt_graphics_unit;

typedef struct {
    const char *const *languages;
    size_t language_count;
    const char *const *words;
    size_t word_count;
    int32_t detect_language;
    int32_t fast;
    int32_t correction;
    double minimum_height;
} swipt_text_options;

void *swipt_graphics_token_create(void);
void swipt_graphics_token_cancel(void *token);

char *swipt_image_inspect(const void *bytes, size_t length, swipt_image_header *header);
char *swipt_image_decode(const void *bytes, size_t length, uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes);
char *swipt_image_encode(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, const char *format, double quality, double scale, swipt_encoded *encoded);

void swipt_vision_warm(uintptr_t logger);
char *swipt_text_recognize(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, swipt_text_options options, void *token, void **text, char **lines);
char *swipt_text_box(void *text, int64_t line, int64_t candidate, int64_t start, int64_t end, double quad[8], bool *found);
char *swipt_barcodes_detect(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, void *token, char **codes);
char *swipt_feature_print(const uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes, int32_t channels, void *token, void **print);
char *swipt_feature_distance(void *first, void *second, double *distance);

void swipt_pixel_buffer_size(void *buffer, int64_t *width, int64_t *height);
char *swipt_pixel_buffer_copy(void *buffer, uint8_t *pixels, int64_t width, int64_t height, int64_t row_bytes);

void *swipt_decoder_create(uintptr_t stream);
bool swipt_decoder_decode(void *decoder, swipt_graphics_unit unit, double arrival, int32_t turns);
void swipt_decoder_close(void *decoder);

char *swipt_movie_open(const char *path, void **reader);
char *swipt_movie_frame(void *reader, double seconds, void *token, void **buffer);

char *swipt_encoder_create(const char *path, bool mp4, int64_t width, int64_t height, void *token, void **encoder);
char *swipt_encoder_append(void *encoder, const uint8_t *pixels, int64_t row_bytes, double milliseconds);
char *swipt_encoder_finish(void *encoder);
void swipt_encoder_cancel(void *encoder);

void swipt_release(void *object);

#endif
