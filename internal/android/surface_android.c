//go:build android && cgo

#include <android/native_window_jni.h>
#include <jni.h>

JNIEXPORT jobject JNICALL Java_io_nekohasekai_swipt_device_Capture_surfaceOf(JNIEnv *env, jclass type, jlong window) {
    return ANativeWindow_toSurface(env, (ANativeWindow *)(uintptr_t)window);
}
