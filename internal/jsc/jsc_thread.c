//go:build cgo

#include "jsc_thread.h"

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

#ifdef __APPLE__
#include <dlfcn.h>
#include <mach/mach_time.h>
#include <objc/objc.h>
#endif

#ifdef _WIN32
#include <windows.h>
#else
#include <pthread.h>
#include <signal.h>
#include <time.h>
#endif

// Private JavaScriptCore API (WebKit 21bb1f66,
// Source/JavaScriptCore/API/JSContextRefPrivate.h:70, 88, 164).
typedef bool (*jsc_should_terminate_callback)(JSContextRef, void *);
typedef void (*jsc_set_time_limit)(JSContextGroupRef, double, jsc_should_terminate_callback, void *);
typedef void (*jsc_set_rejection_callback)(JSGlobalContextRef, JSObjectRef, JSValueRef *);

#ifdef __APPLE__
extern void *objc_autoreleasePoolPush(void);
extern void objc_autoreleasePoolPop(void *pool);
#define jsc_private(name) dlsym(RTLD_DEFAULT, #name)
#define jsc_api_lock(context) NULL
#define jsc_api_unlock(lock) (void)(lock)
#define jsc_loop_create(group) NULL
#define jsc_loop_run(loop) (void)(loop)
#define jsc_loop_stop(loop) (void)(loop)
#define jsc_loop_free(loop) (void)(loop)
#else
extern void JSContextGroupSetExecutionTimeLimit(JSContextGroupRef, double, jsc_should_terminate_callback, void *);
extern void JSGlobalContextSetUnhandledRejectionCallback(JSGlobalContextRef, JSObjectRef, JSValueRef *);
#define jsc_private(name) ((void *)name)

// Bun's WebKit takes no JavaScript lock in some API functions, such as those
// of typed arrays, JSGlobalContextRelease and
// JSGlobalContextSetUnhandledRejectionCallback, while the API calls callbacks
// with every lock dropped (oven-sh/WebKit 5a61071f,
// Source/JavaScriptCore/API/JSTypedArray.cpp:138, 173, 242, 279;
// Source/JavaScriptCore/API/JSContextRef.cpp:176, 277;
// Source/JavaScriptCore/API/APICallbackFunction.h:69).
extern void *jsc_api_lock(JSContextRef context);
extern void jsc_api_unlock(void *lock);
extern void *jsc_loop_create(JSContextGroupRef group);
extern void jsc_loop_run(void *loop);
extern void jsc_loop_stop(void *loop);
extern void jsc_loop_free(void *loop);
#endif

#ifdef _WIN32
typedef HANDLE jsc_thread;
typedef SRWLOCK jsc_mutex;
typedef CONDITION_VARIABLE jsc_cond;
typedef INIT_ONCE jsc_once;
#define JSC_ONCE_INIT INIT_ONCE_STATIC_INIT

static void jsc_mutex_lock(jsc_mutex *mutex) {
	AcquireSRWLockExclusive(mutex);
}

static void jsc_mutex_unlock(jsc_mutex *mutex) {
	ReleaseSRWLockExclusive(mutex);
}

static void jsc_sleep(jsc_cond *cond, jsc_mutex *mutex) {
	SleepConditionVariableSRW(cond, mutex, INFINITE, 0);
}

static void jsc_wake(jsc_cond *cond) {
	WakeAllConditionVariable(cond);
}

static BOOL CALLBACK jsc_run_once(INIT_ONCE *once, void *function, void **context) {
	((void (*)(void))function)();
	return TRUE;
}

static void jsc_call_once(jsc_once *once, void (*function)(void)) {
	InitOnceExecuteOnce(once, jsc_run_once, (void *)function, NULL);
}
#else
typedef pthread_t jsc_thread;
typedef pthread_mutex_t jsc_mutex;
typedef pthread_cond_t jsc_cond;
typedef pthread_once_t jsc_once;
#define JSC_ONCE_INIT PTHREAD_ONCE_INIT

static void jsc_mutex_lock(jsc_mutex *mutex) {
	pthread_mutex_lock(mutex);
}

static void jsc_mutex_unlock(jsc_mutex *mutex) {
	pthread_mutex_unlock(mutex);
}

static void jsc_sleep(jsc_cond *cond, jsc_mutex *mutex) {
	pthread_cond_wait(cond, mutex);
}

static void jsc_wake(jsc_cond *cond) {
	pthread_cond_broadcast(cond);
}

static void jsc_call_once(jsc_once *once, void (*function)(void)) {
	pthread_once(once, function);
}
#endif

struct jsc_runtime {
	jsc_thread thread;
	jsc_mutex mutex;
	jsc_cond cond;
	jsc_command *pending;
	jsc_host_request *host;
	bool quit;
	void *loop;
	bool idle;
	JSContextGroupRef group;
	JSGlobalContextRef context;
	JSValueRef describer;
	JSValueRef framer;
	jsc_set_time_limit set_limit;
	uint64_t run;
	_Atomic uint64_t cancelled;
	bool terminated;
	uintptr_t *released;
	size_t released_count, released_capacity;
};

static _Thread_local jsc_runtime *jsc_current;

static const double jsc_check_interval = 0.05;
static const size_t jsc_stack_size = 8 << 20;

static void jsc_service(jsc_runtime *rt, bool *until);

static JSStringRef jsc_take_string(jsc_text *text) {
	JSStringRef string = text->wide ? JSStringCreateWithCharacters(text->data, text->length) : JSStringCreateWithUTF8CString(text->data);
	free(text->data);
	return string;
}

// The UTF-8 conversion stops at a lone surrogate (WebKit 21bb1f66,
// Source/JavaScriptCore/API/JSStringRef.cpp:89; WebKit 21bb1f66,
// Source/WTF/wtf/unicode/UTF8Conversion.cpp:92).
static jsc_text jsc_text_of(JSStringRef text) {
	size_t length = JSStringGetLength(text);
	size_t capacity = JSStringGetMaximumUTF8CStringSize(text);
	uint8_t *utf8 = malloc(capacity);
	size_t size = JSStringGetUTF8CString(text, (char *)utf8, capacity) - 1;
	size_t units = 0;
	for (size_t index = 0; index < size; index++) {
		units += (utf8[index] & 0xC0) != 0x80;
		units += utf8[index] >= 0xF0;
	}
	jsc_text out = {.data = utf8, .length = size};
	if (units != length) {
		free(utf8);
		JSChar *utf16 = malloc(sizeof(JSChar) * length);
		memcpy(utf16, JSStringGetCharactersPtr(text), sizeof(JSChar) * length);
		out = (jsc_text){.data = utf16, .length = length, .wide = true};
	}
	JSStringRelease(text);
	return out;
}

static JSStringRef jsc_describe_value(jsc_runtime *rt, JSValueRef value) {
	if (rt->describer != NULL) {
		JSValueRef described = JSObjectCallAsFunction(rt->context, (JSObjectRef)rt->describer, NULL, 1, &value, NULL);
		if (described != NULL) {
			value = described;
		}
	}
	JSStringRef text = JSValueToStringCopy(rt->context, value, NULL);
	return text != NULL ? text : JSStringCreateWithUTF8CString("");
}

static JSValueRef jsc_property(JSContextRef ctx, JSValueRef value, const char *name) {
	if (!JSValueIsObject(ctx, value)) {
		return NULL;
	}
	JSStringRef key = JSStringCreateWithUTF8CString(name);
	JSValueRef property = JSObjectGetProperty(ctx, (JSObjectRef)value, key, NULL);
	JSStringRelease(key);
	return property;
}

static jsc_text jsc_string_property(JSContextRef ctx, JSValueRef value, const char *name) {
	JSValueRef property = jsc_property(ctx, value, name);
	if (property == NULL || !JSValueIsString(ctx, property)) {
		return (jsc_text){0};
	}
	return jsc_text_of(JSValueToStringCopy(ctx, property, NULL));
}

static double jsc_number_property(JSContextRef ctx, JSValueRef value, const char *name) {
	JSValueRef property = jsc_property(ctx, value, name);
	return property != NULL && JSValueIsNumber(ctx, property) ? JSValueToNumber(ctx, property, NULL) : 0;
}

static void jsc_capture(jsc_runtime *rt, jsc_command *cmd, JSValueRef exception) {
	JSContextRef ctx = rt->context;
	cmd->exception = true;
	cmd->exception_name = jsc_string_property(ctx, exception, "name");
	cmd->out = jsc_string_property(ctx, exception, "message");
	if (cmd->out.data == NULL) {
		cmd->out = jsc_text_of(jsc_describe_value(rt, exception));
	}
	if (rt->framer == NULL || !JSValueIsObject(ctx, exception)) {
		return;
	}
	JSValueRef frames = JSObjectCallAsFunction(ctx, (JSObjectRef)rt->framer, NULL, 1, &exception, NULL);
	if (frames == NULL || !JSValueIsArray(ctx, frames)) {
		return;
	}
	size_t count = (size_t)jsc_number_property(ctx, frames, "length");
	cmd->frames = calloc(count, sizeof(jsc_frame));
	cmd->frame_count = count;
	for (size_t index = 0; index < count; index++) {
		JSValueRef frame = JSObjectGetPropertyAtIndex(ctx, (JSObjectRef)frames, (unsigned)index, NULL);
		cmd->frames[index] = (jsc_frame){
			.function = jsc_string_property(ctx, frame, "function"),
			.file = jsc_string_property(ctx, frame, "file"),
			.line = jsc_number_property(ctx, frame, "line"),
			.column = jsc_number_property(ctx, frame, "column"),
		};
	}
}

static void jsc_finish(jsc_runtime *rt, jsc_command *cmd, JSValueRef result, JSValueRef exception) {
	if (rt->terminated) {
		cmd->terminated = true;
	} else if (exception != NULL) {
		jsc_capture(rt, cmd, exception);
	} else if (result != NULL) {
		JSValueProtect(rt->context, result);
		cmd->result = result;
	}
}

static void jsc_finish_text(jsc_runtime *rt, jsc_command *cmd, JSStringRef text, JSValueRef exception) {
	if (text != NULL) {
		cmd->out = jsc_text_of(text);
	}
	jsc_finish(rt, cmd, NULL, exception);
}

static void jsc_free_bytes(void *bytes, void *context) {
	free(bytes);
}

static JSValueRef jsc_call_host(JSContextRef ctx, JSObjectRef function, JSObjectRef this_object, size_t count, const JSValueRef arguments[], JSValueRef *exception) {
	jsc_runtime *rt = jsc_current;
	jsc_host_request request = {.handle = (uintptr_t)JSObjectGetPrivate(function), .arguments = arguments, .count = count};
	jsc_mutex_lock(&rt->mutex);
	rt->host = &request;
	jsc_wake(&rt->cond);
	jsc_service(rt, &request.done);
	jsc_mutex_unlock(&rt->mutex);
	void *lock = jsc_api_lock(ctx);
	JSValueRef result = JSValueMakeUndefined(ctx);
	if (request.error.data != NULL) {
		JSStringRef text = jsc_take_string(&request.error);
		JSValueRef message = JSValueMakeString(ctx, text);
		JSStringRelease(text);
		*exception = JSObjectMakeError(ctx, 1, &message, NULL);
	} else if (request.json.data != NULL) {
		JSStringRef text = jsc_take_string(&request.json);
		result = JSValueMakeFromJSONString(ctx, text);
		JSStringRelease(text);
	} else if (request.bytes != NULL) {
		JSObjectRef array = JSObjectMakeTypedArrayWithBytesNoCopy(ctx, kJSTypedArrayTypeUint8Array, request.bytes, request.bytes_length, jsc_free_bytes, NULL, exception);
		if (array != NULL) {
			result = array;
		}
	} else if (request.value != NULL) {
		result = request.value;
		JSValueUnprotect(ctx, result);
	}
	jsc_api_unlock(lock);
	return result;
}

static void jsc_finalize_host(JSObjectRef function) {
	jsc_runtime *rt = jsc_current;
	jsc_mutex_lock(&rt->mutex);
	if (rt->released_count == rt->released_capacity) {
		rt->released_capacity = rt->released_capacity == 0 ? 16 : rt->released_capacity * 2;
		rt->released = realloc(rt->released, sizeof(uintptr_t) * rt->released_capacity);
	}
	rt->released[rt->released_count++] = (uintptr_t)JSObjectGetPrivate(function);
	jsc_mutex_unlock(&rt->mutex);
}

#ifdef __APPLE__
static mach_timebase_info_data_t jsc_timebase;
static jsc_once jsc_timebase_once = JSC_ONCE_INIT;

static void jsc_load_timebase(void) {
	mach_timebase_info(&jsc_timebase);
}

// The same reading and integer conversion as runtime.nanotime (Go 1.26,
// src/runtime/sys_darwin.go:304; src/runtime/sys_darwin_arm64.s:145).
static int64_t jsc_nanotime(void) {
	jsc_call_once(&jsc_timebase_once, jsc_load_timebase);
	int64_t t = (int64_t)mach_absolute_time();
	if (jsc_timebase.numer != 1) {
		t *= (int64_t)jsc_timebase.numer;
	}
	if (jsc_timebase.denom != 1) {
		t /= (int64_t)jsc_timebase.denom;
	}
	return t;
}
#elif defined(_WIN32)
// The same reading and conversion as runtime.nanotime (Go 1.26,
// src/runtime/time_windows.h:13; src/runtime/sys_windows_amd64.s:200;
// src/runtime/sys_windows_arm64.s:149).
static int64_t jsc_nanotime(void) {
	return *(volatile int64_t *)0x7ffe0008 * 100;
}
#else
// The same clock as runtime.nanotime (Go 1.26,
// src/runtime/sys_linux_amd64.s:223; src/runtime/sys_linux_arm64.s:318).
static int64_t jsc_nanotime(void) {
	struct timespec now;
	clock_gettime(CLOCK_MONOTONIC, &now);
	return (int64_t)now.tv_sec * 1000000000 + now.tv_nsec;
}
#endif

static JSValueRef jsc_call_clock(JSContextRef ctx, JSObjectRef function, JSObjectRef this_object, size_t count, const JSValueRef arguments[], JSValueRef *exception) {
	int64_t origin = (int64_t)(intptr_t)JSObjectGetPrivate(function);
	return JSValueMakeNumber(ctx, (double)(jsc_nanotime() - origin) / 1e6);
}

static JSClassRef jsc_class;
static JSClassRef jsc_clock_class;
static jsc_once jsc_class_once = JSC_ONCE_INIT;

static void jsc_create_class(void) {
	JSClassDefinition definition = kJSClassDefinitionEmpty;
	definition.className = "Function";
	definition.callAsFunction = jsc_call_host;
	definition.finalize = jsc_finalize_host;
	jsc_class = JSClassCreate(&definition);
	JSClassDefinition clock = kJSClassDefinitionEmpty;
	clock.className = "Function";
	clock.callAsFunction = jsc_call_clock;
	jsc_clock_class = JSClassCreate(&clock);
}

static JSObjectRef jsc_make_host(jsc_runtime *rt, uintptr_t handle) {
	jsc_call_once(&jsc_class_once, jsc_create_class);
	return JSObjectMake(rt->context, jsc_class, (void *)handle);
}

// JavaScriptCore asks this once its thread has spent the limit in CPU time
// since the limit was set or the outermost call entered it, and after an
// answer of false asks again only if the callback set the limit again (WebKit
// 21bb1f66, Source/JavaScriptCore/runtime/Watchdog.cpp:76, 103, 136; WebKit
// 21bb1f66, Source/JavaScriptCore/runtime/VMEntryScopeInlines.h:37).
static bool jsc_should_terminate(JSContextRef ctx, void *context) {
	jsc_runtime *rt = context;
	uint64_t cancelled = atomic_load(&rt->cancelled);
	if (cancelled != 0 && cancelled == rt->run) {
		rt->terminated = true;
		return true;
	}
	rt->set_limit(rt->group, jsc_check_interval, jsc_should_terminate, rt);
	return false;
}

// A termination in the microtask checkpoint that runs as the outermost call
// returns leaves its exception pending, and the next call answers it without
// running (WebKit 21bb1f66, Source/JavaScriptCore/runtime/VM.cpp:1495;
// WebKit 21bb1f66, Source/JavaScriptCore/runtime/JSLock.cpp:315, 318; Mac14,3,
// macOS 27.0.1 26A434).
static void jsc_recover(jsc_runtime *rt) {
	if (!rt->terminated) {
		return;
	}
	JSStringRef empty = JSStringCreateWithCharacters(NULL, 0);
	JSEvaluateScript(rt->context, empty, NULL, NULL, 1, NULL);
	JSStringRelease(empty);
	rt->terminated = false;
}

#ifdef __linux__
static jsc_once jsc_signal_once = JSC_ONCE_INIT;
static jsc_once jsc_stack_once = JSC_ONCE_INIT;

// Bun's WebKit suspends threads with SIGPWR and warns on replacing a handler
// for it, installs its handlers without SA_ONSTACK as the first VM is made,
// and keeps working once SA_ONSTACK is added to them (oven-sh/WebKit
// 5a61071f, Source/WTF/wtf/posix/ThreadingPOSIX.cpp:239, 304, 341;
// Source/WTF/wtf/threads/Signals.cpp:604;
// Source/JavaScriptCore/runtime/VM.cpp:652).
static void jsc_release_suspend_signal(void) {
	signal(SIGPWR, SIG_DFL);
}

static void jsc_handle_signals_on_stack(void) {
	for (int number = 1; number < NSIG; number++) {
		struct sigaction action;
		if (sigaction(number, NULL, &action) != 0 || !(action.sa_flags & SA_SIGINFO) || (action.sa_flags & SA_ONSTACK)) {
			continue;
		}
		action.sa_flags |= SA_ONSTACK;
		sigaction(number, &action, NULL);
	}
}
#endif

static void jsc_execute(jsc_runtime *rt, jsc_command *cmd) {
	JSContextRef ctx = rt->context;
	JSValueRef exception = NULL;
	switch (cmd->kind) {
	case JSC_CREATE:
#ifdef __linux__
		jsc_call_once(&jsc_signal_once, jsc_release_suspend_signal);
		rt->group = JSContextGroupCreate();
		jsc_call_once(&jsc_stack_once, jsc_handle_signals_on_stack);
#else
		rt->group = JSContextGroupCreate();
#endif
		rt->context = JSGlobalContextCreateInGroup(rt->group, NULL);
		rt->set_limit = (jsc_set_time_limit)jsc_private(JSContextGroupSetExecutionTimeLimit);
		rt->set_limit(rt->group, jsc_check_interval, jsc_should_terminate, rt);
		rt->loop = jsc_loop_create(rt->group);
		break;
	case JSC_DESTROY: {
		jsc_loop_free(rt->loop);
		void *lock = jsc_api_lock(ctx);
		JSGlobalContextRelease(rt->context);
		jsc_api_unlock(lock);
		JSContextGroupRelease(rt->group);
		rt->quit = true;
		break;
	}
	case JSC_BEGIN:
		cmd->run = ++rt->run;
		jsc_recover(rt);
		break;
	case JSC_SET_HOOKS:
		rt->describer = cmd->value;
		rt->framer = cmd->object;
		break;
	case JSC_GLOBAL:
		cmd->result = JSContextGetGlobalObject(ctx);
		break;
	case JSC_NEW_OBJECT: {
		JSObjectRef value = JSObjectMake(ctx, NULL, NULL);
		JSObjectSetPrototype(ctx, value, JSValueMakeNull(ctx));
		JSValueProtect(ctx, value);
		cmd->result = value;
		break;
	}
	case JSC_NEW_STRING: {
		JSStringRef text = jsc_take_string(&cmd->text);
		JSValueRef value = JSValueMakeString(ctx, text);
		JSStringRelease(text);
		JSValueProtect(ctx, value);
		cmd->result = value;
		break;
	}
	case JSC_EVALUATE: {
		JSStringRef script = jsc_take_string(&cmd->text);
		JSStringRef url = jsc_take_string(&cmd->name);
		JSValueRef value = JSEvaluateScript(ctx, script, NULL, url, 1, &exception);
		JSStringRelease(script);
		JSStringRelease(url);
		jsc_finish(rt, cmd, value, exception);
		break;
	}
	case JSC_GET: {
		JSStringRef name = jsc_take_string(&cmd->name);
		JSValueRef value = JSObjectGetProperty(ctx, (JSObjectRef)cmd->object, name, &exception);
		JSStringRelease(name);
		jsc_finish(rt, cmd, value, exception);
		break;
	}
	case JSC_SET: {
		JSStringRef name = jsc_take_string(&cmd->name);
		JSObjectSetProperty(ctx, (JSObjectRef)cmd->object, name, cmd->value, kJSPropertyAttributeReadOnly | kJSPropertyAttributeDontDelete, NULL);
		JSStringRelease(name);
		break;
	}
	case JSC_SET_FUNCTION: {
		JSStringRef name = jsc_take_string(&cmd->name);
		JSObjectSetProperty(ctx, (JSObjectRef)cmd->object, name, jsc_make_host(rt, cmd->handle), kJSPropertyAttributeReadOnly | kJSPropertyAttributeDontDelete, NULL);
		JSStringRelease(name);
		break;
	}
	case JSC_SET_REJECTION: {
		jsc_set_rejection_callback set = (jsc_set_rejection_callback)jsc_private(JSGlobalContextSetUnhandledRejectionCallback);
		set(rt->context, jsc_make_host(rt, cmd->handle), NULL);
		break;
	}
	case JSC_CALL: {
		JSValueRef value = JSObjectCallAsFunction(ctx, (JSObjectRef)cmd->object, NULL, cmd->count, cmd->arguments, &exception);
		jsc_finish(rt, cmd, value, exception);
		break;
	}
	case JSC_TO_STRING:
		jsc_finish_text(rt, cmd, JSValueToStringCopy(ctx, cmd->value, &exception), exception);
		break;
	case JSC_DESCRIBE:
		cmd->out = jsc_text_of(jsc_describe_value(rt, cmd->value));
		break;
	case JSC_IS_FUNCTION:
		cmd->flag = JSValueIsObject(ctx, cmd->value) && JSObjectIsFunction(ctx, (JSObjectRef)cmd->value);
		break;
	case JSC_NUMBER:
		cmd->number = JSValueToNumber(ctx, cmd->value, NULL);
		break;
	case JSC_BYTES: {
		if (JSValueGetTypedArrayType(ctx, cmd->value, NULL) != kJSTypedArrayTypeUint8Array) {
			break;
		}
		JSObjectRef array = (JSObjectRef)cmd->value;
		size_t length = JSObjectGetTypedArrayByteLength(ctx, array, NULL);
		// The pointer is to the start of the buffer rather than of the view
		// (WebKit 21bb1f66, Source/JavaScriptCore/API/JSTypedArray.cpp:257).
		uint8_t *buffer = length > 0 ? JSObjectGetTypedArrayBytesPtr(ctx, array, NULL) : NULL;
		if (length > 0 && buffer == NULL) {
			break;
		}
		cmd->data = malloc(length > 0 ? length : 1);
		if (length > 0) {
			memcpy(cmd->data, buffer + JSObjectGetTypedArrayByteOffset(ctx, array, NULL), length);
		}
		cmd->data_length = length;
		cmd->flag = true;
		break;
	}
	case JSC_PROTECT:
		JSValueProtect(ctx, cmd->value);
		break;
	case JSC_UNPROTECT:
		JSValueUnprotect(ctx, cmd->value);
		break;
	case JSC_NATIVE:
		((jsc_native)cmd->native)(rt->context, cmd->native_command, cmd->value, cmd->native_argument);
		break;
	case JSC_SET_CLOCK: {
		jsc_call_once(&jsc_class_once, jsc_create_class);
		JSStringRef name = jsc_take_string(&cmd->name);
		JSObjectRef clock = JSObjectMake(ctx, jsc_clock_class, (void *)(intptr_t)cmd->native_argument);
		JSObjectSetProperty(ctx, (JSObjectRef)cmd->object, name, clock, kJSPropertyAttributeReadOnly | kJSPropertyAttributeDontDelete, NULL);
		JSStringRelease(name);
		break;
	}
	}
}

static void jsc_service(jsc_runtime *rt, bool *until) {
	while (!*until) {
		jsc_command *cmd = rt->pending;
		if (cmd == NULL && until == &rt->quit && rt->loop != NULL) {
			rt->idle = true;
			jsc_mutex_unlock(&rt->mutex);
			jsc_loop_run(rt->loop);
			jsc_mutex_lock(&rt->mutex);
			continue;
		}
		if (cmd == NULL) {
			jsc_sleep(&rt->cond, &rt->mutex);
			continue;
		}
		rt->pending = NULL;
		jsc_mutex_unlock(&rt->mutex);
#ifdef __APPLE__
		void *pool = objc_autoreleasePoolPush();
		jsc_execute(rt, cmd);
		objc_autoreleasePoolPop(pool);
#else
		bool locked = cmd->kind != JSC_CREATE && cmd->kind != JSC_DESTROY;
		void *lock = locked ? jsc_api_lock(rt->context) : NULL;
		jsc_execute(rt, cmd);
		if (locked) {
			jsc_api_unlock(lock);
		}
#endif
		jsc_mutex_lock(&rt->mutex);
		cmd->done = true;
		jsc_wake(&rt->cond);
	}
}

static void *jsc_thread_main(void *argument) {
	jsc_runtime *rt = argument;
	jsc_current = rt;
	jsc_mutex_lock(&rt->mutex);
	jsc_service(rt, &rt->quit);
	jsc_mutex_unlock(&rt->mutex);
	return NULL;
}

#ifdef _WIN32
static DWORD WINAPI jsc_thread_start(void *argument) {
	jsc_thread_main(argument);
	return 0;
}

jsc_runtime *jsc_runtime_new(void) {
	jsc_runtime *rt = calloc(1, sizeof(jsc_runtime));
	InitializeSRWLock(&rt->mutex);
	InitializeConditionVariable(&rt->cond);
	rt->thread = CreateThread(NULL, jsc_stack_size, jsc_thread_start, rt, STACK_SIZE_PARAM_IS_A_RESERVATION, NULL);
	return rt;
}

void jsc_runtime_free(jsc_runtime *rt) {
	WaitForSingleObject(rt->thread, INFINITE);
	CloseHandle(rt->thread);
	free(rt->released);
	free(rt);
}
#else
jsc_runtime *jsc_runtime_new(void) {
	jsc_runtime *rt = calloc(1, sizeof(jsc_runtime));
	pthread_mutex_init(&rt->mutex, NULL);
	pthread_cond_init(&rt->cond, NULL);
	pthread_attr_t attributes;
	pthread_attr_init(&attributes);
	pthread_attr_setstacksize(&attributes, jsc_stack_size);
	pthread_create(&rt->thread, &attributes, jsc_thread_main, rt);
	pthread_attr_destroy(&attributes);
	return rt;
}

void jsc_runtime_free(jsc_runtime *rt) {
	pthread_join(rt->thread, NULL);
	pthread_mutex_destroy(&rt->mutex);
	pthread_cond_destroy(&rt->cond);
	free(rt->released);
	free(rt);
}
#endif

void jsc_post(jsc_runtime *rt, jsc_command *cmd) {
	jsc_mutex_lock(&rt->mutex);
	rt->pending = cmd;
	jsc_wake(&rt->cond);
	bool idle = rt->idle;
	rt->idle = false;
	jsc_mutex_unlock(&rt->mutex);
	if (idle) {
		jsc_loop_stop(rt->loop);
	}
}

jsc_host_request *jsc_wait(jsc_runtime *rt, jsc_command *cmd) {
	jsc_mutex_lock(&rt->mutex);
	while (rt->host == NULL && !cmd->done) {
		jsc_sleep(&rt->cond, &rt->mutex);
	}
	jsc_host_request *request = rt->host;
	rt->host = NULL;
	jsc_mutex_unlock(&rt->mutex);
	return request;
}

void jsc_cancel(jsc_runtime *rt, uint64_t run) {
	atomic_store(&rt->cancelled, run);
}

void jsc_answer(jsc_runtime *rt, jsc_host_request *request) {
	jsc_mutex_lock(&rt->mutex);
	request->done = true;
	jsc_wake(&rt->cond);
	jsc_mutex_unlock(&rt->mutex);
}

uintptr_t *jsc_take_released(jsc_runtime *rt, size_t *count) {
	jsc_mutex_lock(&rt->mutex);
	uintptr_t *released = rt->released;
	*count = rt->released_count;
	rt->released = NULL;
	rt->released_count = 0;
	rt->released_capacity = 0;
	jsc_mutex_unlock(&rt->mutex);
	return released;
}
