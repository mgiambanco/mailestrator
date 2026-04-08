package com.mailclient.network;

import android.os.Handler;
import android.os.Looper;

import com.google.gson.Gson;
import com.mailclient.data.model.Email;
import com.mailclient.data.model.WebSocketEvent;

import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.TimeUnit;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import okhttp3.WebSocket;
import okhttp3.WebSocketListener;

/**
 * Manages one WebSocket connection per account.
 * Mirrors the wsSessions / listenWebSocket / reconnectWebSocket logic in MailStore.swift.
 */
public class WebSocketManager {

    public interface NewEmailListener {
        void onNewEmail(String accountId, Email email);
    }

    private final OkHttpClient wsClient;
    private final Gson gson = new Gson();
    private final Handler mainHandler = new Handler(Looper.getMainLooper());

    // accountId → active WebSocket
    private final Map<String, WebSocket> sessions = new HashMap<>();
    // accountId → pending ping runnable (so we can cancel on disconnect)
    private final Map<String, Runnable> pingRunnables = new HashMap<>();

    private NewEmailListener listener;

    public WebSocketManager(OkHttpClient baseClient) {
        // Longer read timeout than server's pong-wait so the OS doesn't close
        // the socket while the server is just idle between pings.
        wsClient = baseClient.newBuilder()
                .readTimeout(70, TimeUnit.SECONDS)
                .build();
    }

    public void setNewEmailListener(NewEmailListener l) {
        this.listener = l;
    }

    /** Opens (or re-opens) a WebSocket for the given account. */
    public void connect(String accountId, String token) {
        connect(accountId, token, 0);
    }

    private void connect(String accountId, String token, int attempt) {
        // Token passed as query param — server's requireAuth supports both
        // Authorization header and ?token= query param.
        String url = ApiClient.WS_BASE_URL + "/ws/" + accountId + "?token=" + token;
        Request req = new Request.Builder().url(url).build();

        WebSocket ws = wsClient.newWebSocket(req, new WebSocketListener() {
            @Override
            public void onOpen(WebSocket ws, Response resp) {
                sessions.put(accountId, ws);
                schedulePing(accountId, ws);
            }

            @Override
            public void onMessage(WebSocket ws, String text) {
                WebSocketEvent event = gson.fromJson(text, WebSocketEvent.class);
                if ("new_email".equals(event.type) && event.email != null && listener != null) {
                    mainHandler.post(() -> listener.onNewEmail(accountId, event.email));
                }
            }

            @Override
            public void onFailure(WebSocket ws, Throwable t, Response resp) {
                cancelPing(accountId);
                sessions.remove(accountId);
                scheduleReconnect(accountId, token, attempt);
            }

            @Override
            public void onClosed(WebSocket ws, int code, String reason) {
                cancelPing(accountId);
                sessions.remove(accountId);
                // Normal server-initiated close (e.g. shutdown) — reconnect.
                if (code != 1000) {
                    scheduleReconnect(accountId, token, attempt);
                }
            }
        });
    }

    /** Disconnects and cancels reconnect timers for one account. */
    public void disconnect(String accountId) {
        cancelPing(accountId);
        WebSocket ws = sessions.remove(accountId);
        if (ws != null) ws.close(1000, "going away");
    }

    /** Disconnects all accounts. */
    public void disconnectAll() {
        for (String id : new HashMap<>(sessions).keySet()) disconnect(id);
    }

    // ── Ping keepalive ────────────────────────────────────────────────────────

    private void schedulePing(String accountId, WebSocket ws) {
        Runnable ping = new Runnable() {
            @Override public void run() {
                WebSocket active = sessions.get(accountId);
                if (active != ws) return; // stale reference
                boolean ok = ws.send(""); // empty text frame acts as keepalive
                if (ok) mainHandler.postDelayed(this, 25_000);
                // If send failed, onFailure will handle reconnect.
            }
        };
        pingRunnables.put(accountId, ping);
        mainHandler.postDelayed(ping, 25_000);
    }

    private void cancelPing(String accountId) {
        Runnable r = pingRunnables.remove(accountId);
        if (r != null) mainHandler.removeCallbacks(r);
    }

    // ── Reconnect with exponential backoff ────────────────────────────────────

    /** Delay formula matches iOS: min(3s × 2^attempt, 60s). */
    private void scheduleReconnect(String accountId, String token, int attempt) {
        long delayMs = Math.min(3_000L * (1L << Math.min(attempt, 4)), 60_000L);
        mainHandler.postDelayed(() -> connect(accountId, token, attempt + 1), delayMs);
    }
}
