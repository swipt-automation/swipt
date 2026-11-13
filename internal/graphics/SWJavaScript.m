//go:build darwin && cgo

#import "SWJavaScript.h"

#import <dlfcn.h>

#import "SWError.h"

typedef void (*SWReportCost)(JSContextRef context, size_t bytes);

void SWJavaScriptReportCost(NSInteger bytes) {
    static SWReportCost report;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        report = (SWReportCost)dlsym(RTLD_DEFAULT, "JSReportExtraMemoryCost");
    });
    JSContext *context = JSContext.currentContext;
    if (bytes > 0 && context != nil && report != NULL) {
        report(context.JSGlobalContextRef, (size_t)bytes);
    }
}

JSValue *SWJavaScriptFail(NSError *error) {
    JSContext *context = JSContext.currentContext;
    NSString *name = SWErrorNameOf(error);
    NSString *message = SWErrorMessageOf(error);
    BOOL native = [name isEqualToString:SWErrorNameType] || [name isEqualToString:SWErrorNameRange];
    JSValue *constructor = context[native ? name : @"DOMException"];
    context.exception = native ? [constructor constructWithArguments:@[message]] : [constructor constructWithArguments:@[message, name]];
    return [JSValue valueWithUndefinedInContext:context];
}

static void SWFreeBytes(void *bytes, void *context) {
    free(bytes);
}

JSValue *SWJavaScriptBytes(NSData *data) {
    JSContext *context = JSContext.currentContext;
    size_t count = data.length;
    void *storage = malloc(MAX(count, 1));
    memcpy(storage, data.bytes, count);
    JSObjectRef array = JSObjectMakeTypedArrayWithBytesNoCopy(context.JSGlobalContextRef, kJSTypedArrayTypeUint8Array, storage, count, SWFreeBytes, NULL, NULL);
    if (array == NULL) {
        return SWJavaScriptFail(SWMakeError(SWErrorNameGeneric, @"graphics: make the byte array"));
    }
    return [JSValue valueWithJSValueRef:array inContext:context];
}

NSData *SWJavaScriptData(JSValue *value) {
    JSGlobalContextRef context = value.context.JSGlobalContextRef;
    JSValueRef reference = value.JSValueRef;
    if (context == NULL || reference == NULL || JSValueGetTypedArrayType(context, reference, NULL) != kJSTypedArrayTypeUint8Array) {
        return nil;
    }
    JSObjectRef object = JSValueToObject(context, reference, NULL);
    if (object == NULL) {
        return nil;
    }
    size_t length = JSObjectGetTypedArrayByteLength(context, object, NULL);
    const uint8_t *base = JSObjectGetTypedArrayBytesPtr(context, object, NULL);
    if (length == 0 || base == NULL) {
        return [NSData data];
    }
    return [NSData dataWithBytes:base + JSObjectGetTypedArrayByteOffset(context, object, NULL) length:length];
}
