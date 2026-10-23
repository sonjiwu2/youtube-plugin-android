package ru.youtubeplugin.probe;

import android.app.Activity;
import android.os.Bundle;
import android.widget.TextView;
import java.net.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/** Separate UID: integration checks must travel through Android's per-app VPN. */
public class ProbeActivity extends Activity {
    private final StringBuilder report = new StringBuilder();
    private TextView text;
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        text = new TextView(this); text.setTextSize(14); text.setPadding(24,48,24,24); setContentView(text);
        new Thread(() -> {
            String requested = getIntent().getStringExtra("url");
            String[] urls = requested != null ? new String[]{requested} : new String[]{
                "https://m.youtube.com/", "https://youtubei.googleapis.com/youtubei/v1/browse",
                "https://i.ytimg.com/vi/aqz-KE-bpKQ/hqdefault.jpg", "https://www.google.com/generate_204"};
            for (String url : urls) probe(url);
            publish("DONE");
        }, "network-probe").start();
    }
    private synchronized void publish(String line) {
        report.append(line).append('\n');
        String snapshot = report.toString();
        try (FileOutputStream file = openFileOutput("report.txt", MODE_PRIVATE)) { file.write(snapshot.getBytes(StandardCharsets.UTF_8)); }
        catch (IOException ignored) { }
        runOnUiThread(() -> text.setText(snapshot));
    }
    private void probe(String address) {
        long start = System.nanoTime();
        publish("GET " + address);
        HttpURLConnection connection = null;
        try {
            URL url = new URL(address);
            publish("DNS " + Arrays.toString(InetAddress.getAllByName(url.getHost())));
            connection = (HttpURLConnection) url.openConnection();
            connection.setConnectTimeout(15000); connection.setReadTimeout(20000);
            connection.setRequestProperty("User-Agent", "Mozilla/5.0 (Linux; Android 16) AppleWebKit/537.36 Chrome/140.0.0.0 Mobile Safari/537.36");
            int code = connection.getResponseCode();
            long bytes = 0;
            InputStream stream = code < 400 ? connection.getInputStream() : connection.getErrorStream();
            if (stream != null) try (InputStream input = stream) {
                byte[] buf = new byte[16384]; int n;
                while (bytes < 1_048_576 && (n = input.read(buf)) >= 0) bytes += n;
            }
            publish("HTTP " + code + " bytes=" + bytes + " ms=" + ((System.nanoTime()-start)/1_000_000));
        } catch (Exception error) { publish("ERROR " + error + " ms=" + ((System.nanoTime()-start)/1_000_000)); }
        finally { if (connection != null) connection.disconnect(); }
    }
}
