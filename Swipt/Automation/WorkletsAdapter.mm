// The Expo SwiftPM sync leaves ExpoModulesWorkletsAdapter out when expo-modules-core links as a precompiled XCFramework
// (expo 58.0.22, scripts/spm/plugin.js:229-261; expo-modules-core 58.0.29, spm.config.json:147-154).
#include "../../node_modules/expo-modules-core/ios/WorkletsAdapter/ExpoWorkletsBridgeProvider.mm"
