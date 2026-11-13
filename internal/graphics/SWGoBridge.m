//go:build darwin && cgo

#import "SWGoBridge.h"

#include "_cgo_export.h"

#import "SWError.h"

static char *SWCString(NSString *text) {
    return (char *)text.UTF8String;
}

static NSError *SWTakeFailure(char *failure) {
    NSError *error = SWErrorFromFailure([NSString stringWithUTF8String:failure]);
    free(failure);
    return error;
}

@implementation SWCallContext {
    uintptr_t _scope;
}

- (instancetype)initWithScope:(uintptr_t)scope {
    self = [super init];
    if (self) {
        _scope = scope;
    }
    return self;
}

- (BOOL)cancelled {
    return swiptGraphicsCancelled(_scope);
}

- (NSData *)readRoot:(NSString *)root path:(NSString *)path error:(NSError **)error {
    swipt_graphics_file file = swiptGraphicsReadFile(_scope, SWCString(root), SWCString(path));
    if (file.failure != NULL) {
        SWFailWith(error, SWTakeFailure(file.failure));
        return nil;
    }
    return [NSData dataWithBytesNoCopy:file.bytes length:file.length freeWhenDone:YES];
}

- (BOOL)writeData:(NSData *)data root:(NSString *)root path:(NSString *)path error:(NSError **)error {
    char *failure = swiptGraphicsWriteFile(_scope, SWCString(root), SWCString(path), (void *)data.bytes, data.length);
    if (failure == NULL) {
        return YES;
    }
    SWFailWith(error, SWTakeFailure(failure));
    return NO;
}

- (NSData *)screenshotWithScale:(double *)scale error:(NSError **)error {
    swipt_graphics_screenshot shot = swiptGraphicsScreenshot(_scope);
    if (shot.failure != NULL) {
        SWFailWith(error, SWTakeFailure(shot.failure));
        return nil;
    }
    *scale = shot.scale;
    return [NSData dataWithBytesNoCopy:shot.image length:shot.length freeWhenDone:YES];
}

- (NSInteger)registerObject:(id)object {
    return (NSInteger)swiptGraphicsRegister(_scope, (void *)CFBridgingRetain(object));
}

@end
