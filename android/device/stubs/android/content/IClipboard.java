package android.content;

import android.os.IBinder;
import android.os.RemoteException;

public interface IClipboard extends android.os.IInterface {
    ClipData getPrimaryClip(String callingPackage, String attributionTag, int userId, int deviceId) throws RemoteException;

    void setPrimaryClip(ClipData clip, String callingPackage, String attributionTag, int userId, int deviceId) throws RemoteException;

    void clearPrimaryClip(String callingPackage, String attributionTag, int userId, int deviceId) throws RemoteException;

    void addPrimaryClipChangedListener(IOnPrimaryClipChangedListener listener, String callingPackage, String attributionTag, int userId, int deviceId) throws RemoteException;

    void removePrimaryClipChangedListener(IOnPrimaryClipChangedListener listener, String callingPackage, String attributionTag, int userId, int deviceId) throws RemoteException;

    abstract class Stub extends android.os.Binder implements IClipboard {
        public static IClipboard asInterface(IBinder binder) {
            throw new RuntimeException();
        }
    }
}
