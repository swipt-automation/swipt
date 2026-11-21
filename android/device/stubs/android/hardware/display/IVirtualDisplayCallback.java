package android.hardware.display;

import android.os.IBinder;
import android.os.RemoteException;

public interface IVirtualDisplayCallback extends android.os.IInterface {
    void onPaused() throws RemoteException;

    void onResumed() throws RemoteException;

    void onStopped() throws RemoteException;

    abstract class Stub extends android.os.Binder implements IVirtualDisplayCallback {
        public IBinder asBinder() {
            throw new RuntimeException();
        }
    }
}
