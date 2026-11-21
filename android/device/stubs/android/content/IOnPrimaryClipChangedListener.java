package android.content;

import android.os.IBinder;
import android.os.RemoteException;

public interface IOnPrimaryClipChangedListener extends android.os.IInterface {
    void dispatchPrimaryClipChanged() throws RemoteException;

    abstract class Stub extends android.os.Binder implements IOnPrimaryClipChangedListener {
        public IBinder asBinder() {
            throw new RuntimeException();
        }
    }
}
