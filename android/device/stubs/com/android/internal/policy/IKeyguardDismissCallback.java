package com.android.internal.policy;

import android.os.IBinder;
import android.os.RemoteException;

public interface IKeyguardDismissCallback extends android.os.IInterface {
    void onDismissError() throws RemoteException;

    void onDismissSucceeded() throws RemoteException;

    void onDismissCancelled() throws RemoteException;

    abstract class Stub extends android.os.Binder implements IKeyguardDismissCallback {
        public IBinder asBinder() {
            throw new RuntimeException();
        }
    }
}
