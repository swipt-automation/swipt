//go:build android && cgo

#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <GLES3/gl3.h>
#include <android/native_window.h>
#include <media/NdkImage.h>
#include <media/NdkImageReader.h>
#include <media/NdkMediaCodec.h>
#include <media/NdkMediaExtractor.h>
#include <media/NdkMediaFormat.h>
#include <media/NdkMediaMuxer.h>
#include <math.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "_cgo_export.h"
#include "graphics_android.h"

enum {
    SWIPT_STANDARD_BT709 = 1,
    SWIPT_STANDARD_BT601_PAL = 2,
    SWIPT_STANDARD_BT601_NTSC = 4,
    SWIPT_STANDARD_BT2020 = 6,
    SWIPT_RANGE_FULL = 1,
    SWIPT_RANGE_LIMITED = 2,
    SWIPT_TRANSFER_SDR_VIDEO = 3,
    SWIPT_COLOR_FORMAT_SURFACE = 0x7F000789,
    SWIPT_READER_IMAGES = 4,
};

static char *swipt_media_failure(const char *operation, media_status_t status) {
    char *message = NULL;
    asprintf(&message, "graphics: %s: media status %d", operation, (int)status);
    return message;
}

static void swipt_condition_init(pthread_cond_t *condition) {
    pthread_condattr_t attributes;
    pthread_condattr_init(&attributes);
    pthread_condattr_setclock(&attributes, CLOCK_MONOTONIC);
    pthread_cond_init(condition, &attributes);
    pthread_condattr_destroy(&attributes);
}

static struct timespec swipt_deadline(int64_t wait) {
    struct timespec deadline;
    clock_gettime(CLOCK_MONOTONIC, &deadline);
    int64_t nanoseconds = deadline.tv_nsec + (wait % 1000000) * 1000;
    deadline.tv_sec += (time_t)(wait / 1000000 + nanoseconds / 1000000000);
    deadline.tv_nsec = (long)(nanoseconds % 1000000000);
    return deadline;
}

struct swipt_frames {
    AImageReader *reader;
    ANativeWindow *window;
    bool drawn;
    atomic_int references;
    atomic_int standard;
    atomic_int range;
    pthread_mutex_t lock;
    pthread_cond_t arrived;
    int32_t pending;
    uintptr_t receiver;
};

struct swipt_video_frame {
    swipt_frames *owner;
    AImage *image;
    uint8_t *copy;
    const uint8_t *pixels;
    int32_t pixel_stride;
    const uint8_t *luma;
    const uint8_t *blue;
    const uint8_t *red;
    int32_t luma_stride;
    int32_t chroma_stride;
    int32_t chroma_step;
    int32_t width;
    int32_t height;
    int64_t time;
    int32_t standard;
    int32_t range;
};

static void swipt_frames_release(swipt_frames *frames) {
    if (atomic_fetch_sub(&frames->references, 1) != 1) {
        return;
    }
    AImageReader_delete(frames->reader);
    pthread_cond_destroy(&frames->arrived);
    pthread_mutex_destroy(&frames->lock);
    free(frames);
}

static swipt_video_frame *swipt_drawn_frame_of(swipt_frames *frames, AImage *image) {
    int32_t width = 0;
    int32_t height = 0;
    int64_t time = 0;
    uint8_t *data = NULL;
    int length = 0;
    int32_t stride = 0;
    if (AImage_getWidth(image, &width) != AMEDIA_OK || AImage_getHeight(image, &height) != AMEDIA_OK || AImage_getTimestamp(image, &time) != AMEDIA_OK ||
        AImage_getPlaneData(image, 0, &data, &length) != AMEDIA_OK || AImage_getPlaneRowStride(image, 0, &stride) != AMEDIA_OK) {
        AImage_delete(image);
        return NULL;
    }
    swipt_video_frame *frame = malloc(sizeof(swipt_video_frame));
    if (frame == NULL) {
        AImage_delete(image);
        return NULL;
    }
    *frame = (swipt_video_frame){.owner = frames, .image = image, .pixels = data, .pixel_stride = stride, .width = width, .height = height, .time = time / 1000};
    atomic_fetch_add(&frames->references, 1);
    return frame;
}

static swipt_video_frame *swipt_video_frame_of(swipt_frames *frames, AImage *image) {
    int32_t format = 0;
    int64_t time = 0;
    AImageCropRect crop = {0};
    uint8_t *planes[3] = {0};
    int lengths[3] = {0};
    int32_t strides[3] = {0};
    int32_t steps[3] = {0};
    bool readable = AImage_getFormat(image, &format) == AMEDIA_OK && format == AIMAGE_FORMAT_YUV_420_888 && AImage_getTimestamp(image, &time) == AMEDIA_OK;
    for (int plane = 0; readable && plane < 3; plane++) {
        readable = AImage_getPlaneData(image, plane, &planes[plane], &lengths[plane]) == AMEDIA_OK && AImage_getPlaneRowStride(image, plane, &strides[plane]) == AMEDIA_OK &&
                   AImage_getPlanePixelStride(image, plane, &steps[plane]) == AMEDIA_OK;
    }
    if (!readable || AImage_getCropRect(image, &crop) != AMEDIA_OK) {
        AImage_delete(image);
        return NULL;
    }
    int32_t left = crop.left & ~1;
    int32_t top = crop.top & ~1;
    swipt_video_frame *frame = malloc(sizeof(swipt_video_frame));
    if (frame == NULL) {
        AImage_delete(image);
        return NULL;
    }
    *frame = (swipt_video_frame){
        .owner = frames,
        .image = image,
        .luma = planes[0] + (size_t)top * strides[0] + left,
        .blue = planes[1] + (size_t)(top / 2) * strides[1] + (size_t)(left / 2) * steps[1],
        .red = planes[2] + (size_t)(top / 2) * strides[2] + (size_t)(left / 2) * steps[2],
        .luma_stride = strides[0],
        .chroma_stride = strides[1],
        .chroma_step = steps[1],
        .width = crop.right - left,
        .height = crop.bottom - top,
        .time = time / 1000,
        .standard = atomic_load(&frames->standard),
        .range = atomic_load(&frames->range),
    };
    atomic_fetch_add(&frames->references, 1);
    return frame;
}

static void swipt_frames_image_available(void *context, AImageReader *reader) {
    swipt_frames *frames = context;
    pthread_mutex_lock(&frames->lock);
    if (frames->receiver == 0) {
        frames->pending++;
        pthread_cond_broadcast(&frames->arrived);
    } else {
        AImage *image = NULL;
        if (AImageReader_acquireLatestImage(reader, &image) == AMEDIA_OK) {
            swipt_video_frame *frame = frames->drawn ? swipt_drawn_frame_of(frames, image) : swipt_video_frame_of(frames, image);
            if (frame != NULL) {
                swiptGraphicsScreenFrame(frames->receiver, frame);
            }
        }
    }
    pthread_mutex_unlock(&frames->lock);
}

// The codec's process queues every frame into the reader through binder, so nothing arrives unless
// this process runs binder threads, as app processes and app_process do (Android 14 emulator).
static char *swipt_frames_create(int32_t width, int32_t height, bool drawn, uintptr_t receiver, swipt_frames **created) {
    AImageReader *reader = NULL;
    media_status_t status = drawn ? AImageReader_newWithUsage(width, height, AIMAGE_FORMAT_RGBA_8888, AHARDWAREBUFFER_USAGE_GPU_COLOR_OUTPUT | AHARDWAREBUFFER_USAGE_CPU_READ_OFTEN,
                                                               SWIPT_READER_IMAGES, &reader)
                                  : AImageReader_new(width, height, AIMAGE_FORMAT_YUV_420_888, SWIPT_READER_IMAGES, &reader);
    if (status != AMEDIA_OK) {
        return swipt_media_failure("create the frame reader", status);
    }
    ANativeWindow *window = NULL;
    status = AImageReader_getWindow(reader, &window);
    if (status != AMEDIA_OK) {
        AImageReader_delete(reader);
        return swipt_media_failure("get the frame reader's window", status);
    }
    swipt_frames *frames = calloc(1, sizeof(swipt_frames));
    if (frames == NULL) {
        AImageReader_delete(reader);
        return strdup("graphics: out of memory");
    }
    frames->reader = reader;
    frames->window = window;
    frames->drawn = drawn;
    frames->receiver = receiver;
    atomic_init(&frames->references, 1);
    pthread_mutex_init(&frames->lock, NULL);
    swipt_condition_init(&frames->arrived);
    AImageReader_ImageListener listener = {.context = frames, .onImageAvailable = swipt_frames_image_available};
    AImageReader_setImageListener(reader, &listener);
    *created = frames;
    return NULL;
}

static void swipt_frames_adopt(swipt_frames *frames, AMediaFormat *format) {
    int32_t standard = 0;
    int32_t range = 0;
    AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_COLOR_STANDARD, &standard);
    AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_COLOR_RANGE, &range);
    atomic_store(&frames->standard, standard);
    atomic_store(&frames->range, range);
}

static void swipt_frames_stop(swipt_frames *frames) {
    pthread_mutex_lock(&frames->lock);
    frames->receiver = 0;
    pthread_mutex_unlock(&frames->lock);
}

static int32_t swipt_frames_next(swipt_frames *frames, int64_t wait, swipt_video_frame **frame) {
    struct timespec deadline = swipt_deadline(wait);
    pthread_mutex_lock(&frames->lock);
    while (frames->pending == 0) {
        if (pthread_cond_timedwait(&frames->arrived, &frames->lock, &deadline) != 0) {
            pthread_mutex_unlock(&frames->lock);
            return SWIPT_VIDEO_AGAIN;
        }
    }
    frames->pending--;
    pthread_mutex_unlock(&frames->lock);
    AImage *image = NULL;
    *frame = AImageReader_acquireNextImage(frames->reader, &image) == AMEDIA_OK ? swipt_video_frame_of(frames, image) : NULL;
    return *frame == NULL ? SWIPT_VIDEO_FAILED : SWIPT_VIDEO_DONE;
}

static void swipt_frames_discard(swipt_frames *frames) {
    pthread_mutex_lock(&frames->lock);
    AImage *image = NULL;
    while (AImageReader_acquireNextImage(frames->reader, &image) == AMEDIA_OK) {
        AImage_delete(image);
    }
    frames->pending = 0;
    pthread_mutex_unlock(&frames->lock);
}

void swipt_video_frame_size(swipt_video_frame *frame, int32_t *width, int32_t *height) {
    *width = frame->width;
    *height = frame->height;
}

int64_t swipt_video_frame_time(swipt_video_frame *frame) {
    return frame->time;
}

static uint8_t swipt_clamp(int32_t value) {
    return value < 0 ? 0 : value > 255 ? 255 : (uint8_t)value;
}

void swipt_video_frame_convert(swipt_video_frame *frame, uint8_t *pixels, int32_t width, int32_t height, size_t row_bytes) {
    if (frame->pixels != NULL) {
        for (int32_t row = 0; row < height; row++) {
            memcpy(pixels + (size_t)row * row_bytes, frame->pixels + (size_t)row * frame->pixel_stride, (size_t)width * 4);
        }
        return;
    }
    double red = 0.2126;
    double blue = 0.0722;
    switch (frame->standard) {
    case SWIPT_STANDARD_BT601_PAL:
    case SWIPT_STANDARD_BT601_NTSC:
        red = 0.299;
        blue = 0.114;
        break;
    case SWIPT_STANDARD_BT2020:
        red = 0.2627;
        blue = 0.0593;
        break;
    }
    double green = 1 - red - blue;
    bool full = frame->range == SWIPT_RANGE_FULL;
    double chroma = (full ? 1 : 255.0 / 224) * 65536;
    int32_t offset = full ? 0 : 16;
    int32_t luma = (int32_t)lround((full ? 1 : 255.0 / 219) * 65536);
    int32_t red_cr = (int32_t)lround(2 * (1 - red) * chroma);
    int32_t green_cb = (int32_t)lround(2 * blue * (1 - blue) / green * chroma);
    int32_t green_cr = (int32_t)lround(2 * red * (1 - red) / green * chroma);
    int32_t blue_cb = (int32_t)lround(2 * (1 - blue) * chroma);
    for (int32_t row = 0; row < height; row++) {
        const uint8_t *y = frame->luma + (size_t)row * frame->luma_stride;
        const uint8_t *cb = frame->blue + (size_t)(row / 2) * frame->chroma_stride;
        const uint8_t *cr = frame->red + (size_t)(row / 2) * frame->chroma_stride;
        uint8_t *out = pixels + (size_t)row * row_bytes;
        for (int32_t column = 0; column < width; column++, out += 4) {
            int32_t base = (y[column] - offset) * luma + 32768;
            int32_t u = cb[(column / 2) * frame->chroma_step] - 128;
            int32_t v = cr[(column / 2) * frame->chroma_step] - 128;
            out[0] = swipt_clamp((base + blue_cb * u) >> 16);
            out[1] = swipt_clamp((base - green_cb * u - green_cr * v) >> 16);
            out[2] = swipt_clamp((base + red_cr * v) >> 16);
            out[3] = 255;
        }
    }
}

bool swipt_video_frame_detach(swipt_video_frame *frame) {
    if (frame->image == NULL) {
        return true;
    }
    if (frame->pixels != NULL) {
        size_t row = (size_t)frame->width * 4;
        uint8_t *copy = malloc(row * frame->height);
        if (copy == NULL) {
            return false;
        }
        for (int32_t line = 0; line < frame->height; line++) {
            memcpy(copy + line * row, frame->pixels + (size_t)line * frame->pixel_stride, row);
        }
        AImage_delete(frame->image);
        swipt_frames_release(frame->owner);
        frame->image = NULL;
        frame->owner = NULL;
        frame->copy = copy;
        frame->pixels = copy;
        frame->pixel_stride = (int32_t)row;
        return true;
    }
    int32_t chroma_width = (frame->width + 1) / 2;
    int32_t chroma_height = (frame->height + 1) / 2;
    size_t luma_size = (size_t)frame->width * frame->height;
    size_t chroma_size = (size_t)chroma_width * chroma_height;
    uint8_t *copy = malloc(luma_size + chroma_size * 2);
    if (copy == NULL) {
        return false;
    }
    for (int32_t row = 0; row < frame->height; row++) {
        memcpy(copy + (size_t)row * frame->width, frame->luma + (size_t)row * frame->luma_stride, (size_t)frame->width);
    }
    uint8_t *blue = copy + luma_size;
    uint8_t *red = blue + chroma_size;
    for (int32_t row = 0; row < chroma_height; row++) {
        for (int32_t column = 0; column < chroma_width; column++) {
            size_t source = (size_t)row * frame->chroma_stride + (size_t)column * frame->chroma_step;
            blue[(size_t)row * chroma_width + column] = frame->blue[source];
            red[(size_t)row * chroma_width + column] = frame->red[source];
        }
    }
    AImage_delete(frame->image);
    swipt_frames_release(frame->owner);
    frame->image = NULL;
    frame->owner = NULL;
    frame->copy = copy;
    frame->luma = copy;
    frame->blue = blue;
    frame->red = red;
    frame->luma_stride = frame->width;
    frame->chroma_stride = chroma_width;
    frame->chroma_step = 1;
    return true;
}

void swipt_video_frame_release(swipt_video_frame *frame) {
    if (frame->image != NULL) {
        AImage_delete(frame->image);
        swipt_frames_release(frame->owner);
    }
    free(frame->copy);
    free(frame);
}

// The NDK runs MediaCodec's handler and the asynchronous callbacks on one looper, so a callback
// that calls back into the codec waits on itself (frameworks/av android14-release,
// media/ndk/NdkMediaCodec.cpp:412-444).

static int32_t swipt_decoder_drain(AMediaCodec *codec, swipt_frames *frames, int64_t wait, bool *rendered, bool *ended, media_status_t *error) {
    AMediaCodecBufferInfo info;
    ssize_t index = AMediaCodec_dequeueOutputBuffer(codec, &info, wait);
    if (index == AMEDIACODEC_INFO_OUTPUT_FORMAT_CHANGED) {
        AMediaFormat *format = AMediaCodec_getOutputFormat(codec);
        swipt_frames_adopt(frames, format);
        AMediaFormat_delete(format);
        return SWIPT_VIDEO_AGAIN;
    }
    if (index == AMEDIACODEC_INFO_TRY_AGAIN_LATER || index == AMEDIACODEC_INFO_OUTPUT_BUFFERS_CHANGED) {
        return SWIPT_VIDEO_AGAIN;
    }
    *error = index < 0 ? (media_status_t)index : AMEDIA_OK;
    if (index >= 0) {
        *rendered = info.size > 0 && (info.flags & AMEDIACODEC_BUFFER_FLAG_CODEC_CONFIG) == 0;
        *ended = (info.flags & AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM) != 0;
        *error = AMediaCodec_releaseOutputBuffer(codec, (size_t)index, *rendered);
    }
    return *error == AMEDIA_OK ? SWIPT_VIDEO_DONE : SWIPT_VIDEO_FAILED;
}

char *swipt_screen_frames_create(int32_t width, int32_t height, uintptr_t receiver, swipt_frames **frames, uintptr_t *window) {
    char *failure = swipt_frames_create(width, height, true, receiver, frames);
    if (failure == NULL) {
        *window = (uintptr_t)(*frames)->window;
    }
    return failure;
}

void swipt_screen_frames_close(swipt_frames *frames) {
    swipt_frames_stop(frames);
    swipt_frames_release(frames);
}

struct swipt_movie {
    AMediaExtractor *extractor;
    AMediaCodec *codec;
    swipt_frames *frames;
    bool fed;
    bool input_ended;
    bool output_ended;
    bool rendered;
};

char *swipt_movie_open(int fd, int64_t length, swipt_movie **opened) {
    swipt_movie *movie = calloc(1, sizeof(swipt_movie));
    if (movie == NULL) {
        return strdup("graphics: out of memory");
    }
    movie->extractor = AMediaExtractor_new();
    if (movie->extractor == NULL) {
        swipt_movie_close(movie);
        return strdup("graphics: create the movie extractor");
    }
    media_status_t status = AMediaExtractor_setDataSourceFd(movie->extractor, fd, 0, length);
    if (status != AMEDIA_OK) {
        swipt_movie_close(movie);
        return swipt_media_failure("read the movie", status);
    }
    AMediaFormat *format = NULL;
    const char *mime = NULL;
    for (size_t track = 0; track < AMediaExtractor_getTrackCount(movie->extractor) && format == NULL; track++) {
        AMediaFormat *candidate = AMediaExtractor_getTrackFormat(movie->extractor, track);
        if (AMediaFormat_getString(candidate, AMEDIAFORMAT_KEY_MIME, &mime) && strncmp(mime, "video/", 6) == 0) {
            AMediaExtractor_selectTrack(movie->extractor, track);
            format = candidate;
        } else {
            AMediaFormat_delete(candidate);
        }
    }
    if (format == NULL) {
        swipt_movie_close(movie);
        return strdup("graphics: the movie has no video track");
    }
    int32_t width = 0;
    int32_t height = 0;
    AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_WIDTH, &width);
    AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_HEIGHT, &height);
    char *failure = swipt_frames_create(width, height, false, 0, &movie->frames);
    if (failure == NULL) {
        movie->codec = AMediaCodec_createDecoderByType(mime);
        if (movie->codec == NULL) {
            asprintf(&failure, "graphics: this device has no %s decoder", mime);
        }
    }
    if (failure == NULL) {
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_ROTATION, 0);
        status = AMediaCodec_configure(movie->codec, format, movie->frames->window, NULL, 0);
        if (status == AMEDIA_OK) {
            status = AMediaCodec_start(movie->codec);
        }
        if (status != AMEDIA_OK) {
            failure = swipt_media_failure("start the movie's decoder", status);
        }
    }
    AMediaFormat_delete(format);
    if (failure != NULL) {
        swipt_movie_close(movie);
        return failure;
    }
    *opened = movie;
    return NULL;
}

char *swipt_movie_seek(swipt_movie *movie, int64_t time) {
    media_status_t status = AMediaExtractor_seekTo(movie->extractor, time, AMEDIAEXTRACTOR_SEEK_PREVIOUS_SYNC);
    if (status != AMEDIA_OK) {
        return swipt_media_failure("seek in the movie", status);
    }
    if (movie->fed) {
        status = AMediaCodec_flush(movie->codec);
        if (status != AMEDIA_OK) {
            return swipt_media_failure("flush the movie's decoder", status);
        }
        swipt_frames_discard(movie->frames);
    }
    movie->fed = false;
    movie->input_ended = false;
    movie->output_ended = false;
    movie->rendered = false;
    return NULL;
}

static void swipt_movie_feed(swipt_movie *movie) {
    while (!movie->input_ended) {
        ssize_t index = AMediaCodec_dequeueInputBuffer(movie->codec, 0);
        if (index < 0) {
            return;
        }
        size_t capacity = 0;
        uint8_t *buffer = AMediaCodec_getInputBuffer(movie->codec, (size_t)index, &capacity);
        ssize_t size = buffer == NULL ? -1 : AMediaExtractor_readSampleData(movie->extractor, buffer, capacity);
        if (size < 0) {
            AMediaCodec_queueInputBuffer(movie->codec, (size_t)index, 0, 0, 0, AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM);
            movie->input_ended = true;
            return;
        }
        AMediaCodec_queueInputBuffer(movie->codec, (size_t)index, 0, (size_t)size, (uint64_t)AMediaExtractor_getSampleTime(movie->extractor), 0);
        movie->fed = true;
        AMediaExtractor_advance(movie->extractor);
    }
}

int32_t swipt_movie_next(swipt_movie *movie, int64_t wait, swipt_video_frame **frame, char **failure) {
    if (!movie->rendered) {
        if (movie->output_ended) {
            return SWIPT_VIDEO_ENDED;
        }
        swipt_movie_feed(movie);
        media_status_t error = AMEDIA_OK;
        if (swipt_decoder_drain(movie->codec, movie->frames, wait, &movie->rendered, &movie->output_ended, &error) == SWIPT_VIDEO_FAILED) {
            *failure = swipt_media_failure("decode the movie", error);
            return SWIPT_VIDEO_FAILED;
        }
        if (!movie->rendered) {
            return movie->output_ended ? SWIPT_VIDEO_ENDED : SWIPT_VIDEO_AGAIN;
        }
    }
    int32_t result = swipt_frames_next(movie->frames, wait, frame);
    if (result == SWIPT_VIDEO_FAILED) {
        *failure = strdup("graphics: the movie's decoder rendered no frame");
    }
    movie->rendered = result == SWIPT_VIDEO_AGAIN;
    return result;
}

void swipt_movie_close(swipt_movie *movie) {
    if (movie->codec != NULL) {
        AMediaCodec_stop(movie->codec);
        AMediaCodec_delete(movie->codec);
    }
    if (movie->frames != NULL) {
        swipt_frames_release(movie->frames);
    }
    if (movie->extractor != NULL) {
        AMediaExtractor_delete(movie->extractor);
    }
    free(movie);
}

struct swipt_encoder {
    AMediaMuxer *muxer;
    AMediaCodec *codec;
    ANativeWindow *window;
    EGLDisplay display;
    EGLContext context;
    EGLSurface surface;
    PFNEGLPRESENTATIONTIMEANDROIDPROC present;
    GLuint program;
    GLuint texture;
    GLuint vertices;
    int32_t width;
    int32_t height;
    ssize_t track;
    bool started;
};

static char *swipt_encoder_drain(swipt_encoder *encoder, int64_t wait) {
    for (;;) {
        AMediaCodecBufferInfo info;
        ssize_t index = AMediaCodec_dequeueOutputBuffer(encoder->codec, &info, wait);
        if (index == AMEDIACODEC_INFO_TRY_AGAIN_LATER) {
            return NULL;
        }
        if (index == AMEDIACODEC_INFO_OUTPUT_BUFFERS_CHANGED) {
            continue;
        }
        if (index == AMEDIACODEC_INFO_OUTPUT_FORMAT_CHANGED) {
            AMediaFormat *format = AMediaCodec_getOutputFormat(encoder->codec);
            encoder->track = AMediaMuxer_addTrack(encoder->muxer, format);
            AMediaFormat_delete(format);
            media_status_t status = encoder->track < 0 ? (media_status_t)encoder->track : AMediaMuxer_start(encoder->muxer);
            if (status != AMEDIA_OK) {
                return swipt_media_failure("start the video", status);
            }
            encoder->started = true;
            continue;
        }
        if (index < 0) {
            return swipt_media_failure("encode the video", (media_status_t)index);
        }
        media_status_t status = AMEDIA_OK;
        if (info.size > 0 && (info.flags & AMEDIACODEC_BUFFER_FLAG_CODEC_CONFIG) == 0) {
            size_t size = 0;
            uint8_t *data = AMediaCodec_getOutputBuffer(encoder->codec, (size_t)index, &size);
            status = encoder->started && data != NULL ? AMediaMuxer_writeSampleData(encoder->muxer, (size_t)encoder->track, data, &info) : AMEDIA_ERROR_INVALID_OPERATION;
        }
        AMediaCodec_releaseOutputBuffer(encoder->codec, (size_t)index, false);
        if (status != AMEDIA_OK) {
            return swipt_media_failure("write the video", status);
        }
        if ((info.flags & AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM) != 0) {
            return NULL;
        }
    }
}

static const char swipt_vertex_shader[] = "#version 300 es\n"
                                          "in vec2 position;\n"
                                          "out highp vec2 coordinate;\n"
                                          "void main() {\n"
                                          "    coordinate = vec2(position.x * 0.5 + 0.5, 0.5 - position.y * 0.5);\n"
                                          "    gl_Position = vec4(position, 0.0, 1.0);\n"
                                          "}\n";

static const char swipt_fragment_shader[] = "#version 300 es\n"
                                            "precision highp float;\n"
                                            "uniform sampler2D image;\n"
                                            "in highp vec2 coordinate;\n"
                                            "out vec4 color;\n"
                                            "void main() {\n"
                                            "    color = texture(image, coordinate).bgra;\n"
                                            "}\n";

static char *swipt_egl_failure(const char *operation) {
    char *message = NULL;
    asprintf(&message, "graphics: %s: EGL error 0x%x", operation, eglGetError());
    return message;
}

static GLuint swipt_shader(GLenum kind, const char *source) {
    GLuint shader = glCreateShader(kind);
    glShaderSource(shader, 1, &source, NULL);
    glCompileShader(shader);
    GLint compiled = GL_FALSE;
    glGetShaderiv(shader, GL_COMPILE_STATUS, &compiled);
    if (compiled != GL_TRUE) {
        glDeleteShader(shader);
        return 0;
    }
    return shader;
}

static bool swipt_encoder_bind(swipt_encoder *encoder) {
    return eglMakeCurrent(encoder->display, encoder->surface, encoder->surface, encoder->context) == EGL_TRUE;
}

static void swipt_encoder_unbind(swipt_encoder *encoder) {
    eglMakeCurrent(encoder->display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
}

static char *swipt_encoder_draw_setup(swipt_encoder *encoder) {
    encoder->display = eglGetDisplay(EGL_DEFAULT_DISPLAY);
    if (encoder->display == EGL_NO_DISPLAY || eglInitialize(encoder->display, NULL, NULL) != EGL_TRUE) {
        encoder->display = EGL_NO_DISPLAY;
        return swipt_egl_failure("open the display");
    }
    const EGLint configuration[] = {
        EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_ALPHA_SIZE, 8, EGL_RENDERABLE_TYPE, EGL_OPENGL_ES3_BIT_KHR, EGL_SURFACE_TYPE, EGL_WINDOW_BIT,
        EGL_RECORDABLE_ANDROID, EGL_TRUE, EGL_NONE,
    };
    EGLConfig config = NULL;
    EGLint count = 0;
    if (eglChooseConfig(encoder->display, configuration, &config, 1, &count) != EGL_TRUE || count == 0) {
        return swipt_egl_failure("choose a recordable configuration");
    }
    const EGLint version[] = {EGL_CONTEXT_CLIENT_VERSION, 3, EGL_NONE};
    encoder->context = eglCreateContext(encoder->display, config, EGL_NO_CONTEXT, version);
    if (encoder->context == EGL_NO_CONTEXT) {
        return swipt_egl_failure("create the drawing context");
    }
    encoder->surface = eglCreateWindowSurface(encoder->display, config, encoder->window, NULL);
    if (encoder->surface == EGL_NO_SURFACE) {
        return swipt_egl_failure("draw onto the encoder");
    }
    encoder->present = (PFNEGLPRESENTATIONTIMEANDROIDPROC)eglGetProcAddress("eglPresentationTimeANDROID");
    if (encoder->present == NULL) {
        return strdup("graphics: EGL cannot time the frames");
    }
    if (!swipt_encoder_bind(encoder)) {
        return swipt_egl_failure("draw onto the encoder");
    }
    GLuint vertex = swipt_shader(GL_VERTEX_SHADER, swipt_vertex_shader);
    GLuint fragment = swipt_shader(GL_FRAGMENT_SHADER, swipt_fragment_shader);
    GLint linked = GL_FALSE;
    if (vertex != 0 && fragment != 0) {
        encoder->program = glCreateProgram();
        glAttachShader(encoder->program, vertex);
        glAttachShader(encoder->program, fragment);
        glBindAttribLocation(encoder->program, 0, "position");
        glLinkProgram(encoder->program);
        glGetProgramiv(encoder->program, GL_LINK_STATUS, &linked);
    }
    glDeleteShader(vertex);
    glDeleteShader(fragment);
    if (linked != GL_TRUE) {
        swipt_encoder_unbind(encoder);
        return strdup("graphics: build the shader that draws the frames");
    }
    static const GLfloat corners[] = {-1, -1, 1, -1, -1, 1, 1, 1};
    glGenBuffers(1, &encoder->vertices);
    glBindBuffer(GL_ARRAY_BUFFER, encoder->vertices);
    glBufferData(GL_ARRAY_BUFFER, sizeof(corners), corners, GL_STATIC_DRAW);
    glGenTextures(1, &encoder->texture);
    glBindTexture(GL_TEXTURE_2D, encoder->texture);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    glTexStorage2D(GL_TEXTURE_2D, 1, GL_RGBA8, encoder->width, encoder->height);
    swipt_encoder_unbind(encoder);
    return NULL;
}

static void swipt_encoder_destroy(swipt_encoder *encoder) {
    if (encoder->display != EGL_NO_DISPLAY) {
        if (encoder->program != 0 && swipt_encoder_bind(encoder)) {
            glDeleteTextures(1, &encoder->texture);
            glDeleteBuffers(1, &encoder->vertices);
            glDeleteProgram(encoder->program);
            swipt_encoder_unbind(encoder);
        }
        if (encoder->surface != EGL_NO_SURFACE) {
            eglDestroySurface(encoder->display, encoder->surface);
        }
        if (encoder->context != EGL_NO_CONTEXT) {
            eglDestroyContext(encoder->display, encoder->context);
        }
        eglReleaseThread();
        eglTerminate(encoder->display);
    }
    if (encoder->window != NULL) {
        ANativeWindow_release(encoder->window);
    }
    if (encoder->codec != NULL) {
        AMediaCodec_stop(encoder->codec);
        AMediaCodec_delete(encoder->codec);
    }
    if (encoder->muxer != NULL) {
        AMediaMuxer_delete(encoder->muxer);
    }
    free(encoder);
}

char *swipt_encoder_create(int fd, bool mp4, int32_t width, int32_t height, int32_t frame_rate, swipt_encoder **created) {
    const char *mime = mp4 ? "video/avc" : "video/hevc";
    swipt_encoder *encoder = calloc(1, sizeof(swipt_encoder));
    if (encoder == NULL) {
        return strdup("graphics: out of memory");
    }
    encoder->display = EGL_NO_DISPLAY;
    encoder->context = EGL_NO_CONTEXT;
    encoder->surface = EGL_NO_SURFACE;
    encoder->width = width;
    encoder->height = height;
    char *failure = NULL;
    encoder->muxer = AMediaMuxer_new(fd, AMEDIAMUXER_OUTPUT_FORMAT_MPEG_4);
    if (encoder->muxer == NULL) {
        failure = strdup("graphics: create the video");
    }
    if (failure == NULL) {
        encoder->codec = AMediaCodec_createEncoderByType(mime);
        if (encoder->codec == NULL) {
            asprintf(&failure, "graphics: this device has no %s encoder", mime);
        }
    }
    media_status_t status = AMEDIA_OK;
    const char *operation = NULL;
    if (failure == NULL) {
        // The bits per pixel of AVAssetWriter's default output for a screen recording, measured on macOS 27.2.1.
        double bits_per_pixel = mp4 ? 0.18 : 0.11;
        AMediaFormat *format = AMediaFormat_new();
        AMediaFormat_setString(format, AMEDIAFORMAT_KEY_MIME, mime);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_WIDTH, width);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_HEIGHT, height);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_COLOR_FORMAT, SWIPT_COLOR_FORMAT_SURFACE);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_BIT_RATE, (int32_t)lround((double)width * height * frame_rate * bits_per_pixel));
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_FRAME_RATE, frame_rate);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_I_FRAME_INTERVAL, 1);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_COLOR_STANDARD, SWIPT_STANDARD_BT709);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_COLOR_RANGE, SWIPT_RANGE_LIMITED);
        AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_COLOR_TRANSFER, SWIPT_TRANSFER_SDR_VIDEO);
        status = AMediaCodec_configure(encoder->codec, format, NULL, NULL, AMEDIACODEC_CONFIGURE_FLAG_ENCODE);
        AMediaFormat_delete(format);
        if (status != AMEDIA_OK) {
            asprintf(&failure, "graphics: the %s encoder refused %dx%d pixels: media status %d", mime, width, height, (int)status);
        }
    }
    if (failure == NULL && status == AMEDIA_OK) {
        status = AMediaCodec_createInputSurface(encoder->codec, &encoder->window);
        operation = "create the encoder's surface";
    }
    if (failure == NULL && status == AMEDIA_OK) {
        status = AMediaCodec_start(encoder->codec);
        operation = "start the encoder";
    }
    if (failure == NULL && status != AMEDIA_OK) {
        failure = swipt_media_failure(operation, status);
    }
    if (failure == NULL) {
        failure = swipt_encoder_draw_setup(encoder);
    }
    if (failure != NULL) {
        swipt_encoder_destroy(encoder);
        return failure;
    }
    *created = encoder;
    return NULL;
}

char *swipt_encoder_append(swipt_encoder *encoder, const uint8_t *pixels, size_t row_bytes, int64_t time) {
    if (!swipt_encoder_bind(encoder)) {
        return swipt_egl_failure("draw onto the encoder");
    }
    glViewport(0, 0, encoder->width, encoder->height);
    glUseProgram(encoder->program);
    glActiveTexture(GL_TEXTURE0);
    glBindTexture(GL_TEXTURE_2D, encoder->texture);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, (GLint)(row_bytes / 4));
    glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, encoder->width, encoder->height, GL_RGBA, GL_UNSIGNED_BYTE, pixels);
    glUniform1i(glGetUniformLocation(encoder->program, "image"), 0);
    glBindBuffer(GL_ARRAY_BUFFER, encoder->vertices);
    glEnableVertexAttribArray(0);
    glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 0, NULL);
    glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
    encoder->present(encoder->display, encoder->surface, time);
    bool swapped = eglSwapBuffers(encoder->display, encoder->surface) == EGL_TRUE;
    char *failure = swapped ? NULL : swipt_egl_failure("send the frame to the encoder");
    swipt_encoder_unbind(encoder);
    return failure == NULL ? swipt_encoder_drain(encoder, 0) : failure;
}

char *swipt_encoder_finish(swipt_encoder *encoder) {
    media_status_t status = AMediaCodec_signalEndOfInputStream(encoder->codec);
    char *failure = status == AMEDIA_OK ? swipt_encoder_drain(encoder, -1) : swipt_media_failure("end the video", status);
    if (failure == NULL) {
        AMediaCodec_stop(encoder->codec);
        AMediaCodec_delete(encoder->codec);
        encoder->codec = NULL;
        status = AMediaMuxer_stop(encoder->muxer);
        if (status != AMEDIA_OK) {
            failure = swipt_media_failure("finish the video", status);
        }
    }
    swipt_encoder_destroy(encoder);
    return failure;
}

void swipt_encoder_cancel(swipt_encoder *encoder) {
    swipt_encoder_destroy(encoder);
}
