//go:build darwin && cgo

#import "SWRegions.h"

#import "SWCancelToken.h"
#import "SWRaster.h"

static const NSUInteger SWLargestComponentCount = 10000;

typedef struct {
    NSInteger y;
    NSInteger start;
    NSInteger end;
} SWRun;

CGRect SWComponentRect(SWComponent component) {
    return CGRectMake(component.minX, component.minY, component.maxX - component.minX + 1, component.maxY - component.minY + 1);
}

CGPoint SWComponentCentroid(SWComponent component) {
    return CGPointMake(component.sumX / component.count + 0.5, component.sumY / component.count + 0.5);
}

SWRaster *SWRegionsBinarized(SWRaster *raster, NSError **error) {
    uint8_t table[256];
    for (NSInteger value = 0; value < 256; value++) {
        table[value] = value >= 128 ? 255 : 0;
    }
    return SWRasterLookUp(raster, table, error);
}

static NSInteger SWRoot(NSInteger *parents, NSInteger label) {
    NSInteger current = label;
    while (parents[current] != current) {
        parents[current] = parents[parents[current]];
        current = parents[current];
    }
    return current;
}

static void SWJoin(NSInteger *parents, NSInteger a, NSInteger b) {
    NSInteger first = SWRoot(parents, a);
    NSInteger second = SWRoot(parents, b);
    if (first != second) {
        parents[MAX(first, second)] = MIN(first, second);
    }
}

static void SWMerge(SWComponent *component, SWRun run) {
    NSInteger count = run.end - run.start + 1;
    double sumX = (double)count * (double)(run.start + run.end) / 2;
    double sumY = (double)(count * run.y);
    if (component->count == 0) {
        *component = (SWComponent){.minX = run.start, .minY = run.y, .maxX = run.end, .maxY = run.y, .count = count, .sumX = sumX, .sumY = sumY};
        return;
    }
    component->minX = MIN(component->minX, run.start);
    component->maxX = MAX(component->maxX, run.end);
    component->minY = MIN(component->minY, run.y);
    component->maxY = MAX(component->maxY, run.y);
    component->count += count;
    component->sumX += sumX;
    component->sumY += sumY;
}

NSData *SWRegionsComponents(SWRaster *binary, NSInteger minimumPixels, SWCancelToken *token, NSError **error) {
    NSInteger width = binary.width;
    NSInteger height = binary.height;
    NSInteger rowBytes = binary.rowBytes;
    const uint8_t *base = binary.data;
    NSInteger capacity = 1024;
    NSInteger total = 0;
    SWRun *runs = malloc(capacity * sizeof(SWRun));
    NSInteger *parents = malloc(capacity * sizeof(NSInteger));
    NSInteger previousStart = 0;
    for (NSInteger y = 0; y < height; y++) {
        if (y % 256 == 0 && [token failIfCancelled:error]) {
            free(runs);
            free(parents);
            return nil;
        }
        const uint8_t *row = base + y * rowBytes;
        NSInteger currentStart = total;
        NSInteger x = 0;
        while (x < width) {
            const uint8_t *found = memchr(row + x, 255, width - x);
            if (found == NULL) {
                break;
            }
            NSInteger start = found - row;
            const uint8_t *rest = memchr(row + start, 0, width - start);
            NSInteger end = rest == NULL ? width - 1 : rest - row - 1;
            if (total == capacity) {
                capacity *= 2;
                runs = realloc(runs, capacity * sizeof(SWRun));
                parents = realloc(parents, capacity * sizeof(NSInteger));
            }
            runs[total] = (SWRun){.y = y, .start = start, .end = end};
            parents[total] = total;
            total++;
            x = end + 1;
        }
        NSInteger above = previousStart;
        for (NSInteger current = currentStart; current < total; current++) {
            while (above < currentStart && runs[above].end < runs[current].start - 1) {
                above++;
            }
            for (NSInteger index = above; index < currentStart && runs[index].start <= runs[current].end + 1; index++) {
                SWJoin(parents, current, index);
            }
        }
        previousStart = currentStart;
    }
    SWComponent *merged = calloc(MAX(total, 1), sizeof(SWComponent));
    for (NSInteger index = 0; index < total; index++) {
        SWMerge(&merged[SWRoot(parents, index)], runs[index]);
    }
    free(runs);
    free(parents);
    NSInteger kept = 0;
    for (NSInteger index = 0; index < total; index++) {
        if (merged[index].count > 0 && merged[index].count >= minimumPixels) {
            merged[kept++] = merged[index];
        }
    }
    mergesort_b(merged, kept, sizeof(SWComponent), ^int(const void *a, const void *b) {
        const SWComponent *first = a;
        const SWComponent *second = b;
        if (first->minY != second->minY) {
            return first->minY < second->minY ? -1 : 1;
        }
        return first->minX < second->minX ? -1 : first->minX > second->minX;
    });
    NSData *result = [NSData dataWithBytes:merged length:MIN((NSUInteger)kept, SWLargestComponentCount) * sizeof(SWComponent)];
    free(merged);
    return result;
}

SWRaster *SWRegionsDifference(SWRaster *first, SWRaster *second, NSInteger threshold, NSInteger *changed, NSError **error) {
    NS_VALID_UNTIL_END_OF_SCOPE SWRaster *a = SWRasterGrayscale(first, error);
    if (a == nil) {
        return nil;
    }
    NS_VALID_UNTIL_END_OF_SCOPE SWRaster *b = SWRasterGrayscale(second, error);
    if (b == nil) {
        return nil;
    }
    NSInteger width = a.width;
    NSInteger height = a.height;
    SWRaster *mask = [[SWRaster alloc] initWithWidth:width height:height channels:1];
    const uint8_t *aBase = a.data;
    const uint8_t *bBase = b.data;
    uint8_t *maskBase = mask.data;
    float *left = malloc(MAX(width, 1) * sizeof(float));
    float *right = malloc(MAX(width, 1) * sizeof(float));
    float cut = (float)threshold - 0.5f;
    float half = 127.5f;
    float total = 0;
    vDSP_Length count = (vDSP_Length)width;
    for (NSInteger y = 0; y < height; y++) {
        vDSP_vfltu8(aBase + y * a.rowBytes, 1, left, 1, count);
        vDSP_vfltu8(bBase + y * b.rowBytes, 1, right, 1, count);
        vDSP_vsub(right, 1, left, 1, left, 1, count);
        vDSP_vabs(left, 1, left, 1, count);
        vDSP_vthrsc(left, 1, &cut, &half, left, 1, count);
        vDSP_vsadd(left, 1, &half, left, 1, count);
        float sum = 0;
        vDSP_sve(left, 1, &sum, count);
        total += sum;
        vDSP_vfixu8(left, 1, maskBase + y * mask.rowBytes, 1, count);
    }
    free(left);
    free(right);
    *changed = (NSInteger)roundf(total / 255);
    return mask;
}

NSData *SWRegionsMerged(SWRaster *mask, NSInteger radius, SWCancelToken *token, NSError **error) {
    SWRaster *merged = mask;
    if (radius > 0) {
        SWRaster *padded = [[SWRaster alloc] initWithWidth:mask.width + radius * 2 height:mask.height + radius * 2 channels:1];
        memset(padded.data, 0, padded.rowBytes * padded.height);
        [mask copyInto:padded x:radius y:radius];
        merged = SWRasterMorphed(padded, radius, YES, error);
    }
    if (merged == nil) {
        return nil;
    }
    NSData *components = SWRegionsComponents(merged, 1, token, error);
    if (components == nil) {
        return nil;
    }
    NSUInteger count = components.length / sizeof(SWComponent);
    const SWComponent *items = components.bytes;
    NSMutableData *rects = [NSMutableData dataWithLength:count * sizeof(CGRect)];
    CGRect *out = rects.mutableBytes;
    for (NSUInteger index = 0; index < count; index++) {
        CGRect rect = SWComponentRect(items[index]);
        out[index] = CGRectMake(CGRectGetMinX(rect), CGRectGetMinY(rect), CGRectGetWidth(rect) - radius * 2, CGRectGetHeight(rect) - radius * 2);
    }
    return rects;
}

NSInteger SWRegionsMinimumChange(NSInteger total) {
    return MAX(4, total / 5000);
}
