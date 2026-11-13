#import <Foundation/Foundation.h>

@interface SWCallContext : NSObject
@property (nonatomic, readonly) BOOL cancelled;

- (instancetype)initWithScope:(uintptr_t)scope;
- (NSData *)readRoot:(NSString *)root path:(NSString *)path error:(NSError **)error;
- (BOOL)writeData:(NSData *)data root:(NSString *)root path:(NSString *)path error:(NSError **)error;
- (NSData *)screenshotWithScale:(double *)scale error:(NSError **)error;
- (NSInteger)registerObject:(id)object;
@end
