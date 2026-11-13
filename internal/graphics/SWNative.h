#import <Foundation/Foundation.h>
#import <JavaScriptCore/JavaScriptCore.h>

@protocol SWNativeExports <JSExport>
- (JSValue *)prepare:(NSString *)method :(NSString *)arguments :(JSValue *)objects;
- (JSValue *)take:(NSInteger)identifier;
- (JSValue *)bytes:(NSInteger)identifier;
@end

@interface SWNative : NSObject <SWNativeExports>
- (instancetype)initWithEngine:(uintptr_t)engine;
@end
