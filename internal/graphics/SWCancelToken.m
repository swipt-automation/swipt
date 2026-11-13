//go:build darwin && cgo

#import "SWCancelToken.h"

#import "SWError.h"

@implementation SWCancelToken {
    BOOL (^_cancelled)(void);
}

- (instancetype)initWithCancelled:(BOOL (^)(void))cancelled {
    self = [super init];
    if (self) {
        _cancelled = [cancelled copy];
    }
    return self;
}

- (BOOL)isCancelled {
    return _cancelled();
}

- (BOOL)failIfCancelled:(NSError **)error {
    if (!self.isCancelled) {
        return NO;
    }
    SWFailWith(error, SWErrorAborted());
    return YES;
}

@end
