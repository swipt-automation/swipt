package io.nekohasekai.swipt.privilege;

interface ICoreService {
    void destroy() = 16777114;

    ParcelFileDescriptor connect() = 1;

    void setImportant(IBinder token, boolean important) = 2;
}
