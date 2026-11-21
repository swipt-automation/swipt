package com.android.internal.policy;

import android.os.IBinder;
import android.os.RemoteException;

public interface IKeyguardLockedStateListener extends android.os.IInterface {
    void onKeyguardLockedStateChanged(boolean isKeyguardLocked) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IKeyguardLockedStateListener {
        public IBinder asBinder() {
            throw new RuntimeException();
        }
    }
}
