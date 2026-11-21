package android.hardware.display;

import android.media.projection.IMediaProjection;
import android.os.IBinder;
import android.os.RemoteException;
import android.view.Surface;

public interface IDisplayManager extends android.os.IInterface {
    int createVirtualDisplay(VirtualDisplayConfig virtualDisplayConfig, IVirtualDisplayCallback callback, IMediaProjection projectionToken, String packageName) throws RemoteException;

    void resizeVirtualDisplay(IVirtualDisplayCallback token, int width, int height, int densityDpi) throws RemoteException;

    void setVirtualDisplaySurface(IVirtualDisplayCallback token, Surface surface) throws RemoteException;

    void releaseVirtualDisplay(IVirtualDisplayCallback token) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IDisplayManager {
        public static IDisplayManager asInterface(IBinder binder) {
            throw new RuntimeException();
        }
    }
}
