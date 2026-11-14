//go:build cgo && !darwin

#include "cmakeconfig.h"
#include "jsc_thread.h"
#include <JavaScriptCore/APICast.h>
#include <JavaScriptCore/DeferredWorkTimer.h>
#include <JavaScriptCore/JSLock.h>
#include <JavaScriptCore/VM.h>
#include <wtf/DateMath.h>
#include <wtf/Language.h>
#include <wtf/RunLoop.h>
#include <wtf/TimeZone.h>

extern "C" void *jsc_api_lock(JSContextRef context) {
	return new JSC::JSLockHolder(toJS(context));
}

extern "C" void jsc_api_unlock(void *lock) {
	delete static_cast<JSC::JSLockHolder *>(lock);
}

static constexpr Seconds jsc_idle_delay = 1_s;

struct jsc_loop {
	JSC::VM &vm;
	WTF::RunLoop::Timer idle;
};

// Without Bun's event loop, every thread of Bun's WebKit has a generic run
// loop: its timers fire only while the thread runs it, and a dispatch to it is
// kept until it does. A VM puts its timers on the run loop of the thread that
// made it, and each takes the API lock to fire: deferred work, which calls into
// JavaScript, and the collections and incremental sweeps, which run
// finalizers. Collections come due only as the heap takes new memory or loses
// the last protection of a global context
// (oven-sh/WebKit d6375c72, Source/WTF/wtf/bun/RunLoopBun.cpp:132-151, 159-170;
// Source/WTF/wtf/generic/RunLoopGeneric.cpp:156-191, 305-312;
// Source/WTF/wtf/RunLoop.cpp:157-170;
// Source/JavaScriptCore/runtime/VM.cpp:263, 335;
// Source/JavaScriptCore/runtime/JSRunLoopTimer.cpp:77-81, 219-240;
// Source/JavaScriptCore/runtime/DeferredWorkTimer.cpp:90-96, 158-166;
// Source/JavaScriptCore/heap/GCActivityCallback.cpp:94-103;
// Source/JavaScriptCore/heap/Heap.cpp:624-639, 981, 2578-2590;
// Source/JavaScriptCore/API/JSContextRef.cpp:182-184;
// Source/JavaScriptCore/heap/LocalAllocator.cpp:136;
// Source/JavaScriptCore/heap/IncrementalSweeper.cpp:41-44, 95-110;
// Source/JavaScriptCore/API/JSCallbackObjectFunctions.h:79-92).
extern "C" void *jsc_loop_create(JSContextGroupRef group) {
	JSC::VM &vm = *toJS(group);
	vm.deferredWorkTimer->stopRunningTasks();
	return new jsc_loop{vm, WTF::RunLoop::Timer(vm.runLoop(), "jsc idle"_s, [&vm] {
		JSC::JSLockHolder lock(vm);
		vm.heap.collectAsync(JSC::CollectionScope::Full);
	})};
}

extern "C" void jsc_loop_run(void *loop) {
	auto *it = static_cast<jsc_loop *>(loop);
	it->idle.startOneShot(jsc_idle_delay);
	WTF::RunLoop::run();
	it->idle.stop();
}

extern "C" void jsc_loop_stop(void *loop) {
	Ref runLoop = static_cast<jsc_loop *>(loop)->vm.runLoop();
	runLoop->dispatch([] { WTF::RunLoop::currentSingleton().stop(); });
}

extern "C" void jsc_loop_free(void *loop) {
	delete static_cast<jsc_loop *>(loop);
}

extern "C" void jsc_set_languages(const char *languages) {
	Vector<String> list;
	for (auto language : StringView::fromLatin1(languages).split('\n')) {
		list.append(language.toString());
	}
	WTF::overrideUserPreferredLanguages(list);
}

// A VM drops its date caches when it enters after the time zone ID moves, and
// setting an override does not move it (oven-sh/WebKit d6375c72,
// Source/JavaScriptCore/runtime/VM.h:387, 1019;
// Source/JavaScriptCore/runtime/VM.cpp:2039-2041;
// Source/WTF/wtf/TimeZone.h:38-40; Source/WTF/wtf/DateMath.cpp:1034-1051).
extern "C" void jsc_set_time_zone(const char *time_zone) {
	WTF::setTimeZoneOverride(StringView::fromLatin1(time_zone));
	WTF::timeZoneDidChange();
}
