//go:build cgo

#import <Foundation/Foundation.h>

#include <stdbool.h>
#include <string.h>

typedef struct {
	const void *characters;
	unsigned length;
	bool is8Bit;
} jsc_string_view;

// WebKit overrides the languages it prefers through AppleLanguages in the
// argument domain and reads them again after languageDidChange, and its dates
// take the time zone override over the system's (oven-sh/WebKit 0f29ce70,
// Source/WTF/wtf/cocoa/LanguageCocoa.mm:92-100;
// Source/WTF/wtf/Language.cpp:86-94, 113-128;
// Source/WTF/wtf/text/StringView.h:258-260;
// Source/JavaScriptCore/runtime/JSDateMath.cpp:527).
extern void jsc_language_did_change(void) __asm__("__ZN3WTF17languageDidChangeEv");
extern bool jsc_set_time_zone_override(jsc_string_view) __asm__("__ZN3WTF19setTimeZoneOverrideENS_10StringViewE");

void jsc_set_languages(const char *languages) {
	@autoreleasepool {
		NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
		NSMutableDictionary *arguments = [[[defaults volatileDomainForName:NSArgumentDomain] mutableCopy] autorelease];
		arguments[@"AppleLanguages"] = [@(languages) componentsSeparatedByString:@"\n"];
		[defaults setVolatileDomain:arguments forName:NSArgumentDomain];
	}
	jsc_language_did_change();
}

// Dates keep the time zone they first read, in every context, until
// kCFTimeZoneSystemTimeZoneDidChangeNotification reaches the local center
// (oven-sh/WebKit b83d8a12, Source/WTF/wtf/cocoa/TimeZoneCocoa.cpp:35-48;
// Source/JavaScriptCore/runtime/JSDateMath.h:96-99; observed with the system
// JavaScriptCore of macOS 27.0.1 26A434).
void jsc_set_time_zone(const char *time_zone) {
	jsc_set_time_zone_override((jsc_string_view){.characters = time_zone, .length = (unsigned)strlen(time_zone), .is8Bit = true});
	CFNotificationCenterPostNotification(CFNotificationCenterGetLocalCenter(), kCFTimeZoneSystemTimeZoneDidChangeNotification, NULL, NULL, true);
}
