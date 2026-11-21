#ifndef SWIPT_GRAPHICS_ANDROID_H
#define SWIPT_GRAPHICS_ANDROID_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

enum {
    SWIPT_VIDEO_DONE = 0,
    SWIPT_VIDEO_AGAIN = 1,
    SWIPT_VIDEO_ENDED = 2,
    SWIPT_VIDEO_FAILED = -1,
};

typedef struct swipt_video_frame swipt_video_frame;
typedef struct swipt_frames swipt_frames;
typedef struct swipt_movie swipt_movie;
typedef struct swipt_encoder swipt_encoder;

bool swipt_image_inspect(const void *bytes, size_t length, int32_t *width, int32_t *height);
char *swipt_image_decode(const void *bytes, size_t length, uint8_t *pixels, int32_t width, int32_t height, size_t row_bytes);

void swipt_video_frame_size(swipt_video_frame *frame, int32_t *width, int32_t *height);
int64_t swipt_video_frame_time(swipt_video_frame *frame);
void swipt_video_frame_convert(swipt_video_frame *frame, uint8_t *pixels, int32_t width, int32_t height, size_t row_bytes);
bool swipt_video_frame_detach(swipt_video_frame *frame);
void swipt_video_frame_release(swipt_video_frame *frame);

char *swipt_screen_frames_create(int32_t width, int32_t height, uintptr_t receiver, swipt_frames **frames, uintptr_t *window);
void swipt_screen_frames_close(swipt_frames *frames);

char *swipt_movie_open(int fd, int64_t length, swipt_movie **movie);
char *swipt_movie_seek(swipt_movie *movie, int64_t time);
int32_t swipt_movie_next(swipt_movie *movie, int64_t wait, swipt_video_frame **frame, char **failure);
void swipt_movie_close(swipt_movie *movie);

char *swipt_encoder_create(int fd, bool mp4, int32_t width, int32_t height, int32_t frame_rate, swipt_encoder **encoder);
char *swipt_encoder_append(swipt_encoder *encoder, const uint8_t *pixels, size_t row_bytes, int64_t time);
char *swipt_encoder_finish(swipt_encoder *encoder);
void swipt_encoder_cancel(swipt_encoder *encoder);

#endif
