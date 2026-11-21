package android.app;

import android.content.Intent;
import android.os.Bundle;
import android.os.IBinder;
import android.os.RemoteException;

import java.util.List;

public interface IActivityTaskManager extends android.os.IInterface {
    List<ActivityTaskManager.RootTaskInfo> getAllRootTaskInfosOnDisplay(int displayId) throws RemoteException;

    WaitResult startActivityAndWait(IApplicationThread caller, String callingPackage, String callingFeatureId, Intent intent, String resolvedType, IBinder resultTo, String resultWho, int requestCode, int flags, ProfilerInfo profilerInfo, Bundle options, int userId) throws RemoteException;

    void registerTaskStackListener(ITaskStackListener listener) throws RemoteException;

    void unregisterTaskStackListener(ITaskStackListener listener) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IActivityTaskManager {
        public static IActivityTaskManager asInterface(IBinder binder) {
            throw new RuntimeException();
        }
    }
}
