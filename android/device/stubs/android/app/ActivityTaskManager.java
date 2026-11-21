package android.app;

public final class ActivityTaskManager {
    public static class RootTaskInfo extends TaskInfo {
        public int getWindowingMode() {
            throw new RuntimeException();
        }
    }
}
