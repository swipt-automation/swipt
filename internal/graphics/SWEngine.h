#import <Foundation/Foundation.h>

@class JSContext;
@class SWCall;
@class SWScreenOwner;

@interface SWEngine : NSObject
@property (nonatomic, readonly) SWScreenOwner *screens;

- (instancetype)initWithContext:(JSContext *)context;
- (NSInteger)prepare:(SWCall *)call;
- (SWCall *)takePrepared:(NSInteger)identifier;
- (NSInteger)registerObject:(id)object;
- (id)take:(NSInteger)identifier;
- (void)drop:(NSInteger)identifier;
- (void)close;
@end
