#import <Foundation/Foundation.h>
#import <objc/runtime.h>

static void ignore(id self, SEL selector) {}

static NSDictionary *noSettings(id self, SEL selector)
{
  return @{@"settings" : @{}};
}

static void replace(NSString *className, NSArray<NSString *> *selectors, IMP implementation)
{
  Class moduleClass = NSClassFromString(className);
  for (NSString *selector in selectors) {
    method_setImplementation(class_getInstanceMethod(moduleClass, NSSelectorFromString(selector)), implementation);
  }
}

__attribute__((constructor)) static void guardSwipletModules(void)
{
  replace(@"RCTI18nManager", @[ @"allowRTL:", @"forceRTL:", @"swapLeftAndRightInRTL:" ], (IMP)ignore);
  replace(@"RCTAppearance", @[ @"setColorScheme:" ], (IMP)ignore);
  replace(@"RCTSettingsManager", @[ @"setValues:", @"deleteValues:", @"userDefaultsDidChange:" ], (IMP)ignore);
  replace(@"RCTSettingsManager", @[ @"getConstants", @"constantsToExport" ], (IMP)noSettings);
}
