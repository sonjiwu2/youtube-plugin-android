package ru.youtubeplugin.probe;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.widget.TextView;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;

/** Local share target used to verify report export from the non-debuggable APK. */
public class ReportActivity extends Activity {
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        String report = getIntent().getStringExtra(Intent.EXTRA_TEXT);
        TextView text = new TextView(this);
        text.setPadding(24,48,24,24);
        try (FileOutputStream output = openFileOutput("shared-report.txt", MODE_PRIVATE)) {
            if (report == null) throw new IllegalArgumentException("No report supplied");
            output.write(report.getBytes(StandardCharsets.UTF_8));
            text.setText("Отчёт получен\n\n" + report);
        } catch (Exception error) { text.setText(error.toString()); }
        setContentView(text);
    }
}
