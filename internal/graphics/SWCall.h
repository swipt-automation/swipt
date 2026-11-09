#import <Foundation/Foundation.h>

@class SWArguments;
@class SWCallContext;
@class SWCancelToken;
@class SWEngine;
@class SWImageNode;
@class SWScreenRecording;

@interface SWCall : NSObject
@property (nonatomic, readonly) NSString *method;
@property (nonatomic, readonly) SWArguments *arguments;
@property (nonatomic, readonly) NSArray *objects;
@property (nonatomic) SWEngine *engine;
@property (nonatomic) SWCallContext *context;
@property (nonatomic) SWCancelToken *token;

- (instancetype)initWithMethod:(NSString *)method arguments:(SWArguments *)arguments objects:(NSArray *)objects;
- (SWImageNode *)image:(NSUInteger)index error:(NSError **)error;
- (SWScreenRecording *)recording:(NSError **)error;
@end

typedef id (*SWCallFunction)(SWCall *call, NSError **error);

id SWCallExecute(SWCall *call, NSError **error);
BOOL SWCallScreenFrames(SWCall *call, NSError **error);

id SWCallDecode(SWCall *call, NSError **error);
id SWCallEncode(SWCall *call, NSError **error);
id SWCallSave(SWCall *call, NSError **error);
id SWCallPixel(SWCall *call, NSError **error);
id SWCallStats(SWCall *call, NSError **error);
id SWCallFind(SWCall *call, NSError **error);
id SWCallColors(SWCall *call, NSError **error);
id SWCallComponents(SWCall *call, NSError **error);

id SWCallText(SWCall *call, NSError **error);
id SWCallDiff(SWCall *call, NSError **error);
id SWCallDistance(SWCall *call, NSError **error);
id SWCallNearest(SWCall *call, NSError **error);
id SWCallBarcodes(SWCall *call, NSError **error);
id SWCallDifference(SWImageNode *first, SWImageNode *second, NSInteger threshold, SWCancelToken *token, NSError **error);

id SWCallScreenshot(SWCall *call, NSError **error);
id SWCallScreenOpen(SWCall *call, NSError **error);
id SWCallScreenClose(SWCall *call, NSError **error);
id SWCallScreenFrame(SWCall *call, NSError **error);
id SWCallScreenRecord(SWCall *call, NSError **error);

id SWCallRecordingStop(SWCall *call, NSError **error);
id SWCallRecordingOpen(SWCall *call, NSError **error);
id SWCallRecordingExport(SWCall *call, NSError **error);

extern const NSInteger SWDefaultDifferenceThreshold;
