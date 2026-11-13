#import <CoreGraphics/CoreGraphics.h>
#import <Foundation/Foundation.h>
#import <Vision/Vision.h>

@class SWArguments;
@class SWCancelToken;
@class SWImageNode;

@interface SWRecognizedText : NSObject
@property (nonatomic, readonly) NSArray<NSDictionary<NSString *, id> *> *lines;

- (NSDictionary<NSString *, id> *)boundingBoxLine:(NSInteger)line candidate:(NSInteger)candidate start:(NSInteger)start end:(NSInteger)end error:(NSError **)error;
@end

void SWRecognitionWarm(void (^done)(NSError *failure));
SWRecognizedText *SWRecognizeText(SWImageNode *node, SWArguments *arguments, SWCancelToken *token, NSError **error);
VNFeaturePrintObservation *SWFeaturePrint(SWImageNode *node, SWCancelToken *token, NSError **error);
BOOL SWFeatureDistance(VNFeaturePrintObservation *reference, SWImageNode *other, double *distance, SWCancelToken *token, NSError **error);
NSArray<NSDictionary<NSString *, id> *> *SWDetectBarcodes(SWImageNode *node, SWCancelToken *token, NSError **error);
