package ru.razgon.app;

import android.app.Activity;
import android.net.VpnService;
import android.os.Bundle;

/** Shell-only controls for repeatable emulator tests; absent from release APKs. */
public class QaActivity extends Activity {
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        if (VpnService.prepare(this) != null) { finish(); return; }
        getSharedPreferences("plugin", MODE_PRIVATE).edit()
            .putString("strategy", getIntent().getStringExtra("strategy"))
            .putString("dns", "auto").putBoolean("enabled", true)
            .putBoolean("reconnect", false).commit();
        PluginVpnService.Companion.start(this, true);
        finish();
    }
}
