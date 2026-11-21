package io.nekohasekai.swipt.app

import android.app.Application
import android.content.res.Configuration
import android.os.LocaleList
import com.facebook.react.ReactNativeApplicationEntryPoint.loadReactNative
import expo.modules.ApplicationLifecycleDispatcher
import io.nekohasekai.swipt.automation.SwipletHost
import io.nekohasekai.swipt.automation.SwipletSession
import io.nekohasekai.swipt.automation.SwipletStore
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.core.CoreFailures
import io.nekohasekai.swipt.developer.API
import io.nekohasekai.swipt.privilege.Privilege
import io.nekohasekai.swipt.runtime.SwiptHostModule

class SwiptApplication : Application() {
    private lateinit var locales: LocaleList

    override fun onCreate() {
        super.onCreate()
        application = this
        locales = resources.configuration.locales
        loadReactNative(this)
        ApplicationLifecycleDispatcher.onApplicationCreate(this)
        SwiptHostModule.openSession = ::SwipletSession
        Privilege.start(this)
        Core.start()
        CoreFailures.start(this)
        API.start()
        SwipletStore.start()
        SwipletHost.start(this)
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        if (newConfig.locales == locales) return
        locales = newConfig.locales
        SwipletStore.reload()
    }

    companion object {
        lateinit var application: SwiptApplication
            private set
    }
}
