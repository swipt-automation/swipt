//go:build darwin && cgo

#import "SWDepacketizer.h"

#import "SWAccessUnit.h"

@implementation SWDepacketizer {
    SWAccessUnit *_current;
    NSMutableData *_fragment;
    BOOL _hasSequence;
    uint16_t _lastSequence;
}

- (void)reset {
    _current = nil;
    _fragment = nil;
    _hasSequence = NO;
}

- (void)addAggregated:(const uint8_t *)payload length:(NSInteger)length to:(SWAccessUnit *)unit {
    NSInteger at = 2;
    while (at + 2 <= length) {
        NSInteger size = (NSInteger)payload[at] << 8 | payload[at + 1];
        at += 2;
        if (size <= 0 || at + size > length) {
            unit.damaged = YES;
            break;
        }
        [unit.nals addObject:[NSData dataWithBytes:payload + at length:size]];
        at += size;
    }
}

- (void)addFragment:(const uint8_t *)payload length:(NSInteger)length to:(SWAccessUnit *)unit {
    if (length <= 3) {
        unit.damaged = YES;
        return;
    }
    uint8_t header = payload[2];
    if ((header & 0x80) != 0) {
        if (_fragment != nil) {
            unit.damaged = YES;
        }
        uint8_t start[2] = {(uint8_t)((payload[0] & 0x81) | ((header & 0x3f) << 1)), payload[1]};
        _fragment = [NSMutableData dataWithBytes:start length:2];
        [_fragment appendBytes:payload + 3 length:length - 3];
    } else if (_fragment != nil) {
        [_fragment appendBytes:payload + 3 length:length - 3];
    } else {
        unit.damaged = YES;
    }
    if ((header & 0x40) != 0 && _fragment != nil) {
        [unit.nals addObject:_fragment];
        _fragment = nil;
    }
}

- (void)push:(const uint8_t *)bytes length:(NSInteger)length arrival:(double)arrival into:(NSMutableArray<SWAccessUnit *> *)done {
    if (length < 12 || bytes[0] >> 6 != 2) {
        return;
    }
    BOOL marker = (bytes[1] & 0x80) != 0;
    uint16_t sequence = (uint16_t)(bytes[2] << 8 | bytes[3]);
    uint32_t timestamp = (uint32_t)bytes[4] << 24 | (uint32_t)bytes[5] << 16 | (uint32_t)bytes[6] << 8 | bytes[7];
    NSInteger offset = 12 + 4 * (bytes[0] & 0x0f);
    uint16_t profile = 0;
    if ((bytes[0] & 0x10) != 0) {
        if (length < offset + 4) {
            return;
        }
        profile = (uint16_t)(bytes[offset] << 8 | bytes[offset + 1]);
        offset += 4 + 4 * (NSInteger)(bytes[offset + 2] << 8 | bytes[offset + 3]);
    }
    NSInteger end = length;
    if ((bytes[0] & 0x20) != 0) {
        end -= bytes[end - 1];
    }
    if (offset + 2 > end) {
        return;
    }
    BOOL gap = _hasSequence && sequence != (uint16_t)(_lastSequence + 1);
    _hasSequence = YES;
    _lastSequence = sequence;
    if (_current != nil && _current.rtpTimestamp != timestamp) {
        if (_fragment != nil) {
            _current.damaged = YES;
            _fragment = nil;
        }
        [done addObject:_current];
        _current = nil;
    }
    SWAccessUnit *unit = _current ?: [[SWAccessUnit alloc] initWithTimestamp:timestamp arrival:arrival profile:profile];
    if (gap) {
        unit.damaged = YES;
        _fragment = nil;
    }
    const uint8_t *payload = bytes + offset;
    NSInteger size = end - offset;
    switch ((payload[0] >> 1) & 0x3f) {
    case 48:
        [self addAggregated:payload length:size to:unit];
        break;
    case 49:
        [self addFragment:payload length:size to:unit];
        break;
    default:
        [unit.nals addObject:[NSData dataWithBytes:payload length:size]];
        break;
    }
    if (marker) {
        [done addObject:unit];
        _current = nil;
        _fragment = nil;
    } else {
        _current = unit;
    }
}

@end
