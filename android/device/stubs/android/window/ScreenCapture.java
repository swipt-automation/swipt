package android.window;

import android.graphics.ColorSpace;
import android.hardware.HardwareBuffer;

public final class ScreenCapture {
    public static SynchronousScreenCaptureListener createSyncCaptureListener() {
        throw new RuntimeException();
    }

    public static class CaptureArgs {
        public static class Builder {
            public Builder setCaptureSecureLayers(boolean captureSecureLayers) {
                throw new RuntimeException();
            }

            public CaptureArgs build() {
                throw new RuntimeException();
            }
        }
    }

    public static class ScreenCaptureListener {
    }

    public abstract static class SynchronousScreenCaptureListener extends ScreenCaptureListener {
        public abstract ScreenshotHardwareBuffer getBuffer();
    }

    public static class ScreenshotHardwareBuffer {
        public HardwareBuffer getHardwareBuffer() {
            throw new RuntimeException();
        }

        public ColorSpace getColorSpace() {
            throw new RuntimeException();
        }
    }
}
