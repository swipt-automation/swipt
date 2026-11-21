package android.app;

import android.content.res.Configuration;
import android.os.IBinder;
import android.os.RemoteException;

import java.util.List;

public interface IActivityManager extends android.os.IInterface {
    void forceStopPackage(String packageName, int userId) throws RemoteException;

    int getCurrentUserId() throws RemoteException;

    Configuration getConfiguration() throws RemoteException;

    List<ActivityManager.RunningAppProcessInfo> getRunningAppProcesses() throws RemoteException;

    void registerProcessObserver(IProcessObserver observer) throws RemoteException;

    void unregisterProcessObserver(IProcessObserver observer) throws RemoteException;

    ContentProviderHolder getContentProviderExternal(String name, int userId, IBinder token, String tag) throws RemoteException;

    void removeContentProviderExternalAsUser(String name, IBinder token, int userId) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IActivityManager {
        public static IActivityManager asInterface(IBinder binder) {
            throw new RuntimeException();
        }
    }
}
