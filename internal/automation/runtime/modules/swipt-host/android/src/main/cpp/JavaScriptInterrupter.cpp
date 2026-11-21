#include <jni.h>
#include <jsi/hermes-interfaces.h>
#include <jsi/jsi.h>

// A Hermes timeout ends only the current execution, and only in code with async break checks:
// react-native 0.88.1, ReactCommon/jsi/jsi/hermes-interfaces.h:177-186.
extern "C" JNIEXPORT jboolean JNICALL
Java_io_nekohasekai_swipt_runtime_JavaScriptInterrupter_triggerTimeout(JNIEnv *, jclass, jlong runtime) {
  auto hermes = facebook::jsi::castInterface<facebook::hermes::IHermes>(reinterpret_cast<facebook::jsi::Runtime *>(runtime));
  if (hermes == nullptr) {
    return JNI_FALSE;
  }
  hermes->asyncTriggerTimeout();
  return JNI_TRUE;
}
