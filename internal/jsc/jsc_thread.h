//go:build cgo

#ifndef JSC_THREAD_H
#define JSC_THREAD_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#ifdef __APPLE__
#include <JavaScriptCore/JavaScriptCore.h>
#else
#include <JavaScriptCore/JavaScript.h>
#endif

typedef enum {
	JSC_CREATE,
	JSC_DESTROY,
	JSC_BEGIN,
	JSC_SET_HOOKS,
	JSC_GLOBAL,
	JSC_NEW_OBJECT,
	JSC_NEW_STRING,
	JSC_EVALUATE,
	JSC_GET,
	JSC_SET,
	JSC_SET_FUNCTION,
	JSC_SET_REJECTION,
	JSC_CALL,
	JSC_TO_STRING,
	JSC_DESCRIBE,
	JSC_IS_FUNCTION,
	JSC_NUMBER,
	JSC_BYTES,
	JSC_PROTECT,
	JSC_UNPROTECT,
	JSC_NATIVE,
	JSC_SET_CLOCK,
} jsc_kind;

typedef struct {
	void *data;
	size_t length;
	bool wide;
} jsc_text;

typedef struct {
	jsc_text function;
	jsc_text file;
	double line;
	double column;
} jsc_frame;

typedef void (*jsc_native)(JSGlobalContextRef context, int32_t command, JSValueRef value, uint64_t argument);

typedef struct {
	jsc_kind kind;
	JSValueRef object;
	JSValueRef value;
	JSValueRef *arguments;
	size_t count;
	jsc_text text;
	jsc_text name;
	uintptr_t handle;
	uintptr_t native;
	int32_t native_command;
	uint64_t native_argument;

	JSValueRef result;
	jsc_text out;
	jsc_text exception_name;
	jsc_frame *frames;
	size_t frame_count;
	uint8_t *data;
	size_t data_length;
	double number;
	uint64_t run;
	bool flag;
	bool exception;
	bool terminated;
	bool done;
} jsc_command;

typedef struct {
	uintptr_t handle;
	const JSValueRef *arguments;
	size_t count;
	jsc_text json;
	uint8_t *bytes;
	size_t bytes_length;
	JSValueRef value;
	jsc_text error;
	bool done;
} jsc_host_request;

typedef struct jsc_runtime jsc_runtime;

#ifdef __cplusplus
extern "C" {
#endif

jsc_runtime *jsc_runtime_new(void);
void jsc_runtime_free(jsc_runtime *rt);
void jsc_post(jsc_runtime *rt, jsc_command *cmd);
jsc_host_request *jsc_wait(jsc_runtime *rt, jsc_command *cmd);
void jsc_answer(jsc_runtime *rt, jsc_host_request *request);
void jsc_cancel(jsc_runtime *rt, uint64_t run);
uintptr_t *jsc_take_released(jsc_runtime *rt, size_t *count);
void jsc_set_languages(const char *languages);
void jsc_set_time_zone(const char *time_zone);

#ifdef __cplusplus
}
#endif

#endif
