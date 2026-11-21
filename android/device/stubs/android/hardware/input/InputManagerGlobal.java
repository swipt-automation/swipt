package android.hardware.input;

import android.view.InputEvent;

public final class InputManagerGlobal {
    public static InputManagerGlobal getInstance() {
        throw new RuntimeException();
    }

    public boolean injectInputEvent(InputEvent event, int mode) {
        throw new RuntimeException();
    }
}
