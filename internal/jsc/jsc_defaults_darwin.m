//go:build cgo

#import <Foundation/Foundation.h>

#include <dlfcn.h>
#include <stdbool.h>
#include <string.h>

// WTF::StringView, which setTimeZoneOverride takes by value, as a build without
// assertions lays it out (WebKit-7625.2.8.11.9, the source of the
// JavaScriptCore of macOS 27.2.1 26B131, Source/WTF/wtf/text/StringView.h:39-43,
// 54-64, 252-254).
typedef struct {
	const void *characters;
	unsigned length;
	bool is8Bit;
} jsc_string_view;

typedef void (*jsc_language_did_change)(void);
typedef bool (*jsc_set_time_zone_override)(jsc_string_view);

// WebKit overrides the languages it prefers through AppleLanguages in the
// argument domain, and reads them again after languageDidChange
// (WebKit-7625.2.8.11.9, Source/WTF/wtf/cocoa/LanguageCocoa.mm:88-96;
// Source/WTF/wtf/Language.cpp:77-90, 106-121;
// Source/WTF/wtf/cf/LanguageCF.cpp:88-90).
void jsc_set_languages(const char *languages) {
	@autoreleasepool {
		NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
		NSMutableDictionary *arguments = [[[defaults volatileDomainForName:NSArgumentDomain] mutableCopy] autorelease];
		arguments[@"AppleLanguages"] = [@(languages) componentsSeparatedByString:@"\n"];
		[defaults setVolatileDomain:arguments forName:NSArgumentDomain];
	}
	jsc_language_did_change did_change = (jsc_language_did_change)dlsym(RTLD_DEFAULT, "_ZN3WTF17languageDidChangeEv");
	if (did_change != NULL) {
		did_change();
	}
}

// Dates take the time zone override over the host's, and keep the time zone
// they first read, in every context, until
// kCFTimeZoneSystemTimeZoneDidChangeNotification reaches the local center
// (WebKit-7625.2.8.11.9, Source/JavaScriptCore/runtime/JSDateMath.cpp:461-476,
// 487-497; Source/JavaScriptCore/runtime/JSDateMath.h:94-111).
void jsc_set_time_zone(const char *time_zone) {
	jsc_set_time_zone_override set_override = (jsc_set_time_zone_override)dlsym(RTLD_DEFAULT, "_ZN3WTF19setTimeZoneOverrideENS_10StringViewE");
	if (set_override == NULL) {
		return;
	}
	set_override((jsc_string_view){.characters = time_zone, .length = (unsigned)strlen(time_zone), .is8Bit = true});
	CFNotificationCenterPostNotification(CFNotificationCenterGetLocalCenter(), kCFTimeZoneSystemTimeZoneDidChangeNotification, NULL, NULL, true);
}
