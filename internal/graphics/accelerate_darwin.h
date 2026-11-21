#ifndef SWIPT_ACCELERATE_H
#define SWIPT_ACCELERATE_H

#include <stdbool.h>
#include <stdint.h>
#include <CoreGraphics/CoreGraphics.h>

long swipt_scale(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_width, long out_height, long out_row_bytes, int channels);
long swipt_turn(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, int channels, int quarters);
CGImageRef swipt_image_create(const uint8_t *in, long width, long height, long row_bytes, int channels, long *status) CF_RETURNS_RETAINED;
long swipt_rotate(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_width, long out_height, long out_row_bytes, int channels, double degrees);
long swipt_mix(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, const int16_t weights[4], int32_t divisor);
long swipt_map(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, const uint8_t table[256]);
long swipt_map_channels(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, const uint8_t tables[4][256]);
long swipt_histograms(const uint8_t *in, long width, long height, long row_bytes, int channels, uint64_t *counts);
void swipt_absolute_difference(const uint8_t *a, long a_row_bytes, const uint8_t *b, long b_row_bytes, long width, long height, uint8_t *out, long out_row_bytes);
long swipt_extreme(const uint8_t *in, long width, long height, long row_bytes, uint8_t *out, long out_row_bytes, int channels, long size, bool maximum);
void swipt_plane_of(const uint8_t *in, long width, long height, long row_bytes, float *out);
long swipt_plane_scale(const float *in, long width, long height, float *out, long out_width, long out_height);
void swipt_plane_square(const float *in, float *out, long count);
float swipt_sum(const float *values, long count);
float swipt_dot(const float *a, const float *b, long count);
void swipt_centered_product(const float *values, const float *weights, float mean, float *out, long count);
void swipt_correlate(const float *image, long width, long height, const float *kernel, long kernel_width, long kernel_height, float *out);
void swipt_window_sums(const float *image, const float *squares, long width, const float *centered, const float *weights, long kernel_width, long kernel_height,
                       const int64_t *origins, long count, float *out);
void swipt_color_window_sums(const uint8_t *target, long row_bytes, long x, long y, const float *first, const float *second, const float *third, const float *weights, long width,
                             long height, float *out);

#endif
