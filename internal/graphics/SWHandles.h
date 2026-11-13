#import <Foundation/Foundation.h>
#import <JavaScriptCore/JavaScriptCore.h>

@class SWImageNode;
@class SWRecognizedText;
@class SWScreenRecording;
@class SWImageHandle;

@protocol SWImageExports <JSExport>
@property (nonatomic, readonly) double width;
@property (nonatomic, readonly) double height;
@property (nonatomic, readonly) double scale;
@property (nonatomic, readonly) NSArray<NSNumber *> *frame;
@property (nonatomic, readonly) double time;
@property (nonatomic, readonly) double timestamp;
@property (nonatomic, readonly) NSInteger pixelWidth;
@property (nonatomic, readonly) NSInteger pixelHeight;
- (JSValue *)crop:(double)x :(double)y :(double)width :(double)height;
- (SWImageHandle *)resize:(NSInteger)width :(NSInteger)height;
- (SWImageHandle *)rotate:(double)degrees;
- (SWImageHandle *)grayscale;
- (SWImageHandle *)threshold:(NSInteger)level;
- (SWImageHandle *)mask:(NSInteger)lowRed :(NSInteger)lowGreen :(NSInteger)lowBlue :(NSInteger)highRed :(NSInteger)highGreen :(NSInteger)highBlue;
- (SWImageHandle *)invert;
- (SWImageHandle *)dilate:(NSInteger)radius;
- (SWImageHandle *)erode:(NSInteger)radius;
- (JSValue *)showBytes;
@end

@interface SWImageHandle : NSObject <SWImageExports>
@property (nonatomic, readonly) SWImageNode *node;

- (instancetype)initWithNode:(SWImageNode *)node;
@end

@protocol SWResourceExports <JSExport>
@end

@interface SWResourceHandle : NSObject <SWResourceExports>
@property (nonatomic, readonly) uintptr_t handle;

- (instancetype)initWithHandle:(uintptr_t)handle;
@end

@protocol SWScreenRecordingExports <JSExport>
@property (nonatomic, readonly) NSString *info;
- (SWImageHandle *)frame:(double)time;
@end

@interface SWScreenRecordingHandle : SWResourceHandle <SWScreenRecordingExports>
- (instancetype)initWithHandle:(uintptr_t)handle recording:(SWScreenRecording *)recording;
@end

@protocol SWTextExports <JSExport>
- (JSValue *)boundingBox:(NSInteger)line :(NSInteger)candidate :(NSInteger)start :(NSInteger)end;
@end

@interface SWTextHandle : NSObject <SWTextExports>
- (instancetype)initWithText:(SWRecognizedText *)text;
@end
