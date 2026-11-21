package android.app;

import android.os.IBinder;
import android.os.RemoteException;

public interface IProcessObserver extends android.os.IInterface {
    void onProcessStarted(int pid, int processUid, int packageUid, String packageName, String processName) throws RemoteException;

    void onForegroundActivitiesChanged(int pid, int uid, boolean foregroundActivities) throws RemoteException;

    void onForegroundServicesChanged(int pid, int uid, int serviceTypes) throws RemoteException;

    void onProcessDied(int pid, int uid) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IProcessObserver {
        public IBinder asBinder() {
            throw new RuntimeException();
        }
    }
}
