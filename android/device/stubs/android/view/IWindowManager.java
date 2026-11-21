package android.view;

import android.os.IBinder;
import android.os.RemoteException;
import android.window.ScreenCapture;
import android.window.ScreenCaptureInternal;

import com.android.internal.policy.IKeyguardDismissCallback;
import com.android.internal.policy.IKeyguardLockedStateListener;

public interface IWindowManager extends android.os.IInterface {
    void captureDisplay(int displayId, ScreenCapture.CaptureArgs captureArgs, ScreenCapture.ScreenCaptureListener listener) throws RemoteException;

    void captureDisplay(int displayId, ScreenCaptureInternal.CaptureArgs captureArgs, ScreenCaptureInternal.ScreenCaptureListener listener) throws RemoteException;

    boolean isKeyguardLocked() throws RemoteException;

    boolean isKeyguardSecure(int userId) throws RemoteException;

    void lockNow(android.os.Bundle options) throws RemoteException;

    void dismissKeyguard(IKeyguardDismissCallback callback, CharSequence message) throws RemoteException;

    void addKeyguardLockedStateListener(IKeyguardLockedStateListener listener) throws RemoteException;

    void removeKeyguardLockedStateListener(IKeyguardLockedStateListener listener) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IWindowManager {
        public static IWindowManager asInterface(IBinder binder) {
            throw new RuntimeException();
        }
    }
}
