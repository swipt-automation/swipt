package com.android.internal.statusbar;

import android.os.IBinder;
import android.os.RemoteException;

public interface IStatusBarService extends android.os.IInterface {
    void onNotificationClick(String key, NotificationVisibility nv) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IStatusBarService {
        public static IStatusBarService asInterface(IBinder binder) {
            throw new RuntimeException();
        }
    }
}
