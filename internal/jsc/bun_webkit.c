//go:build cgo && !darwin

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define ZSTD_STATIC_LINKING_ONLY
#include <zstd.h>

#ifdef _WIN32
#include <windows.h>

static SRWLOCK bun_icu_lock = SRWLOCK_INIT;

static void bun_icu_acquire(void) {
	AcquireSRWLockExclusive(&bun_icu_lock);
}

static void bun_icu_release(void) {
	ReleaseSRWLockExclusive(&bun_icu_lock);
}
#else
#include <pthread.h>

static pthread_mutex_t bun_icu_lock = PTHREAD_MUTEX_INITIALIZER;

static void bun_icu_acquire(void) {
	pthread_mutex_lock(&bun_icu_lock);
}

static void bun_icu_release(void) {
	pthread_mutex_unlock(&bun_icu_lock);
}
#endif

// Bun's ICU keeps some data items as zstd frames made with this dictionary,
// and reads each item it loads, with its length, from what this hook returns
// for it (oven-sh/WebKit d6375c72, icu/udata-decompress-hook.patch;
// oven-sh/bun v1.4.4, src/jsc/bindings/bun_icu_decompress.cpp:115).
extern const unsigned char bun_icu_zstd_dict[];
extern const unsigned int bun_icu_zstd_dict_size;

typedef struct {
	const void *item;
	void *data;
	int32_t length;
} bun_icu_item;

static bun_icu_item *bun_icu_items;
static size_t bun_icu_count, bun_icu_capacity;
static ZSTD_DCtx *bun_icu_context;
static ZSTD_DDict *bun_icu_dictionary;

static const void *bun_icu_decompress(const void *item, int32_t *length) {
	for (size_t index = 0; index < bun_icu_count; index++) {
		if (bun_icu_items[index].item == item) {
			*length = bun_icu_items[index].length;
			return bun_icu_items[index].data;
		}
	}
	if (bun_icu_context == NULL) {
		bun_icu_context = ZSTD_createDCtx();
		bun_icu_dictionary = ZSTD_createDDict_byReference(bun_icu_zstd_dict, bun_icu_zstd_dict_size);
	}
	size_t compressed = ZSTD_findFrameCompressedSize(item, *length > 0 ? (size_t)*length : 1 << 20);
	if (ZSTD_isError(compressed)) {
		return item;
	}
	unsigned long long size = ZSTD_getFrameContentSize(item, compressed);
	if (size == ZSTD_CONTENTSIZE_UNKNOWN || size == ZSTD_CONTENTSIZE_ERROR) {
		return item;
	}
	void *data = malloc(size);
	if (ZSTD_isError(ZSTD_decompress_usingDDict(bun_icu_context, data, size, item, compressed, bun_icu_dictionary))) {
		free(data);
		return item;
	}
	if (bun_icu_count == bun_icu_capacity) {
		bun_icu_capacity = bun_icu_capacity == 0 ? 64 : bun_icu_capacity * 2;
		bun_icu_items = realloc(bun_icu_items, sizeof(bun_icu_item) * bun_icu_capacity);
	}
	bun_icu_items[bun_icu_count++] = (bun_icu_item){.item = item, .data = data, .length = (int32_t)size};
	*length = (int32_t)size;
	return data;
}

const void *bun_icu_maybe_decompress(const void *item, int32_t *length) {
	uint32_t magic;
	if (item == NULL) {
		return item;
	}
	memcpy(&magic, item, sizeof(magic));
	if (magic != ZSTD_MAGICNUMBER) {
		return item;
	}
	bun_icu_acquire();
	const void *result = bun_icu_decompress(item, length);
	bun_icu_release();
	return result;
}
