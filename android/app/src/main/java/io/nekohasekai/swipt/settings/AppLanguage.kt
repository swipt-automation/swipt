package io.nekohasekai.swipt.settings

import android.app.LocaleConfig
import android.app.LocaleManager
import android.icu.text.Collator
import android.icu.text.DisplayContext
import android.icu.text.LocaleDisplayNames
import android.icu.util.ULocale
import android.os.LocaleList
import io.nekohasekai.swipt.app.SwiptApplication

object AppLanguage {
    private val localeManager: LocaleManager get() = SwiptApplication.application.getSystemService(LocaleManager::class.java)

    val available: List<String>
        get() {
            val locales = LocaleConfig.fromContextIgnoringOverride(SwiptApplication.application).supportedLocales ?: return emptyList()
            val collator = Collator.getInstance()
            return (0 until locales.size()).map { locales[it].toLanguageTag() }.distinct().sortedWith { left, right ->
                collator.compare(name(left), name(right))
            }
        }

    val selected: String?
        get() {
            val current = localeManager.applicationLocales.takeIf { !it.isEmpty }?.get(0) ?: return null
            val available = available
            current.toLanguageTag().let { tag -> if (tag in available) return tag }
            val wanted = ULocale.addLikelySubtags(ULocale.forLocale(current))
            val candidates = available.map { it to ULocale.addLikelySubtags(ULocale.forLanguageTag(it)) }
                .filter { (_, locale) -> locale.language == wanted.language && locale.script == wanted.script }
            return (candidates.firstOrNull { (_, locale) -> locale.country == wanted.country } ?: candidates.firstOrNull())?.first
        }

    fun select(code: String?) {
        localeManager.applicationLocales = if (code == null) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(code)
    }

    fun name(code: String): String {
        val locale = ULocale.forLanguageTag(code)
        return LocaleDisplayNames.getInstance(locale, DisplayContext.CAPITALIZATION_FOR_UI_LIST_OR_MENU).localeDisplayName(locale)
    }
}
