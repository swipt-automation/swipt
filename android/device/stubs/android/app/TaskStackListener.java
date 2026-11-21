package android.app;

import android.os.IBinder;
import android.os.RemoteException;

public abstract class TaskStackListener extends android.os.Binder implements ITaskStackListener {
    public void onTaskStackChanged() throws RemoteException {
    }

    public void onTaskFocusChanged(int taskId, boolean focused) throws RemoteException {
    }

    public IBinder asBinder() {
        throw new RuntimeException();
    }
}
