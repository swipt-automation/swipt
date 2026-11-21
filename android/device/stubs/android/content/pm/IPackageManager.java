package android.content.pm;

import android.content.Intent;
import android.os.RemoteException;

public interface IPackageManager extends android.os.IInterface {
    PackageInfo getPackageInfo(String packageName, long flags, int userId) throws RemoteException;

    ParceledListSlice<ResolveInfo> queryIntentActivities(Intent intent, String resolvedType, long flags, int userId) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IPackageManager {
        public static IPackageManager asInterface(android.os.IBinder binder) {
            throw new RuntimeException();
        }
    }
}
