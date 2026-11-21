package io.nekohasekai.swipt.runtime

import com.facebook.react.bridge.ReactContext
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

object JavaScriptInterrupter {
  init {
    System.loadLibrary("swipt-runtime")
  }

  /**
   * The instance must stay alive meanwhile: the runtime goes away when React Native destroys it
   * (react-native 0.88.1, ReactAndroid/src/main/java/com/facebook/react/runtime/ReactInstance.kt:419-427).
   */
  fun interruptUntilIdle(context: ReactContext): Int {
    val idle = CountDownLatch(1)
    context.runOnJSQueueThread { idle.countDown() }
    var interruptions = 0
    while (!idle.await(100, TimeUnit.MILLISECONDS)) {
      val runtime = context.javaScriptContextHolder?.get() ?: 0
      if (runtime == 0L) {
        break
      }
      if (!triggerTimeout(runtime)) {
        return -1
      }
      interruptions++
    }
    return interruptions
  }

  @JvmStatic
  private external fun triggerTimeout(runtime: Long): Boolean
}
