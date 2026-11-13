//go:build darwin && cgo

#import "SWError.h"

#import "SWJSON.h"

NSString *const SWErrorNameGeneric = @"Error";
NSString *const SWErrorNameType = @"TypeError";
NSString *const SWErrorNameRange = @"RangeError";
NSString *const SWErrorNameData = @"DataError";
NSString *const SWErrorNameTimeout = @"TimeoutError";
NSString *const SWErrorNameUnavailable = @"UnavailableError";
NSString *const SWErrorNameState = @"InvalidStateError";
NSString *const SWErrorNameAbort = @"AbortError";
NSString *const SWErrorNameNotFound = @"NotFoundError";

static NSString *const SWErrorDomain = @"io.nekohasekai.swipt.graphics";
static NSString *const SWErrorNameKey = @"name";

NSError *SWMakeError(NSString *name, NSString *message) {
    return [NSError errorWithDomain:SWErrorDomain code:0 userInfo:@{SWErrorNameKey: name, NSLocalizedDescriptionKey: message}];
}

void SWFail(NSError **error, NSString *name, NSString *format, ...) {
    if (error == NULL) {
        return;
    }
    va_list arguments;
    va_start(arguments, format);
    NSString *message = [[NSString alloc] initWithFormat:format arguments:arguments];
    va_end(arguments);
    *error = SWMakeError(name, message);
}

void SWFailCause(NSError **error, NSString *name, NSError *cause, NSString *format, ...) {
    if (error == NULL) {
        return;
    }
    va_list arguments;
    va_start(arguments, format);
    NSString *message = [[NSString alloc] initWithFormat:format arguments:arguments];
    va_end(arguments);
    NSString *reason = SWErrorMessageOf(cause);
    *error = SWMakeError(name, reason == nil ? message : [NSString stringWithFormat:@"%@: %@", message, reason]);
}

void SWFailWith(NSError **error, NSError *cause) {
    if (error != NULL) {
        *error = cause;
    }
}

NSError *SWErrorStatus(OSStatus status) {
    return [NSError errorWithDomain:NSOSStatusErrorDomain code:status userInfo:nil];
}

NSError *SWErrorFromFailure(NSString *failure) {
    NSDictionary *decoded = SWDecodeJSON(failure);
    NSString *name = nil;
    NSString *message = nil;
    if ([decoded isKindOfClass:NSDictionary.class]) {
        name = [decoded[@"name"] isKindOfClass:NSString.class] ? decoded[@"name"] : nil;
        message = [decoded[@"message"] isKindOfClass:NSString.class] ? decoded[@"message"] : nil;
    }
    return SWMakeError(name ?: SWErrorNameGeneric, message ?: failure);
}

NSError *SWErrorAborted(void) {
    return SWMakeError(SWErrorNameAbort, @"the operation was aborted");
}

NSError *SWErrorEnded(void) {
    return SWMakeError(SWErrorNameAbort, @"the JavaScript session ended");
}

NSString *SWErrorNameOf(NSError *error) {
    if ([error.domain isEqualToString:SWErrorDomain]) {
        return error.userInfo[SWErrorNameKey];
    }
    return SWErrorNameGeneric;
}

static NSString *SWErrorLevelOf(NSError *error) {
    // A Swift error bridged to NSError is a __SwiftNativeNSError, whose -description describes the Swift value
    // (swift 8cc0db7, stdlib/public/runtime/ErrorObject.mm:70, 103-116).
    if ([error isKindOfClass:NSClassFromString(@"__SwiftNativeNSError")]) {
        return error.description;
    }
    NSMutableArray<NSString *> *details = [NSMutableArray array];
    NSString *description = error.userInfo[NSLocalizedDescriptionKey];
    if (description == nil && ![error.domain isEqualToString:NSOSStatusErrorDomain]) {
        description = error.localizedDescription;
    }
    for (NSString *detail in @[description ?: @"", error.userInfo[NSLocalizedFailureReasonErrorKey] ?: @"", error.userInfo[NSDebugDescriptionErrorKey] ?: @""]) {
        if (detail.length > 0 && ![details containsObject:detail]) {
            [details addObject:detail];
        }
    }
    NSString *identity = [NSString stringWithFormat:@"%@ %ld", error.domain, (long)error.code];
    return details.count == 0 ? identity : [NSString stringWithFormat:@"%@ (%@)", [details componentsJoinedByString:@": "], identity];
}

NSString *SWErrorMessageOf(NSError *error) {
    if (error == nil) {
        return nil;
    }
    if ([error.domain isEqualToString:SWErrorDomain]) {
        return error.localizedDescription;
    }
    NSMutableArray<NSString *> *causes = [NSMutableArray array];
    for (NSError *cause in error.underlyingErrors) {
        [causes addObject:SWErrorMessageOf(cause)];
    }
    NSString *level = SWErrorLevelOf(error);
    return causes.count == 0 ? level : [NSString stringWithFormat:@"%@: %@", level, [causes componentsJoinedByString:@"; "]];
}
