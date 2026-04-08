package com.mailclient.network;

import com.google.gson.Gson;
import com.google.gson.reflect.TypeToken;
import com.mailclient.data.model.*;

import org.json.JSONException;
import org.json.JSONObject;

import java.io.IOException;
import java.lang.reflect.Type;
import java.util.List;
import java.util.concurrent.TimeUnit;

import okhttp3.*;

/**
 * Singleton HTTP client. All methods are asynchronous (OkHttp enqueue).
 * Mirrors APIClient.swift.
 */
public class ApiClient {

    private static ApiClient instance;

    // Change these to your server's address.
    public static String BASE_URL   = "http://10.0.2.2:8080";  // emulator → host
    public static String WS_BASE_URL = "ws://10.0.2.2:8080";

    private final OkHttpClient client;
    private final Gson gson = new Gson();

    private ApiClient() {
        client = new OkHttpClient.Builder()
                .connectTimeout(15, TimeUnit.SECONDS)
                .readTimeout(30, TimeUnit.SECONDS)
                .writeTimeout(15, TimeUnit.SECONDS)
                .build();
    }

    public static synchronized ApiClient getInstance() {
        if (instance == null) instance = new ApiClient();
        return instance;
    }

    /** Returns the underlying OkHttpClient (used by WebSocketManager). */
    public OkHttpClient getHttpClient() {
        return client;
    }

    // ── Accounts ─────────────────────────────────────────────────────────────

    public void createAccount(ApiCallback<Account> cb) {
        Request req = new Request.Builder()
                .url(BASE_URL + "/accounts")
                .post(RequestBody.create(new byte[0]))
                .build();
        client.newCall(req).enqueue(new Callback() {
            @Override public void onFailure(Call call, IOException e) { cb.onFailure(e.getMessage()); }
            @Override public void onResponse(Call call, Response resp) throws IOException {
                try (ResponseBody body = resp.body()) {
                    if (!resp.isSuccessful() || body == null) { cb.onFailure("HTTP " + resp.code()); return; }
                    // Server returns {id, address, token}; map to Account.
                    JSONObject json = new JSONObject(body.string());
                    Account a = new Account();
                    a.id      = json.getString("id");
                    a.address = json.getString("address");
                    a.token   = json.getString("token");
                    cb.onSuccess(a);
                } catch (Exception e) { cb.onFailure(e.getMessage()); }
            }
        });
    }

    public void deleteAccount(String accountId, String token, ApiCallback<Void> cb) {
        Request req = authRequest("accounts/" + accountId, "DELETE", token, null);
        client.newCall(req).enqueue(voidCallback(cb));
    }

    // ── Emails ───────────────────────────────────────────────────────────────

    public void listEmails(String accountId, String token,
                           int limit, String beforeCursor,
                           ApiCallback<EmailPage> cb) {
        HttpUrl.Builder url = HttpUrl.parse(BASE_URL + "/accounts/" + accountId + "/emails")
                .newBuilder()
                .addQueryParameter("limit", String.valueOf(limit));
        if (beforeCursor != null) url.addQueryParameter("before", beforeCursor);

        Request req = new Request.Builder()
                .url(url.build())
                .header("Authorization", "Bearer " + token)
                .get()
                .build();
        client.newCall(req).enqueue(new Callback() {
            @Override public void onFailure(Call call, IOException e) { cb.onFailure(e.getMessage()); }
            @Override public void onResponse(Call call, Response resp) throws IOException {
                try (ResponseBody body = resp.body()) {
                    if (!resp.isSuccessful() || body == null) { cb.onFailure("HTTP " + resp.code()); return; }
                    cb.onSuccess(gson.fromJson(body.string(), EmailPage.class));
                }
            }
        });
    }

    public void getEmail(String accountId, String emailId, String token, ApiCallback<Email> cb) {
        Request req = authRequest("accounts/" + accountId + "/emails/" + emailId, "GET", token, null);
        client.newCall(req).enqueue(new Callback() {
            @Override public void onFailure(Call call, IOException e) { cb.onFailure(e.getMessage()); }
            @Override public void onResponse(Call call, Response resp) throws IOException {
                try (ResponseBody body = resp.body()) {
                    if (!resp.isSuccessful() || body == null) { cb.onFailure("HTTP " + resp.code()); return; }
                    cb.onSuccess(gson.fromJson(body.string(), Email.class));
                }
            }
        });
    }

    public void deleteEmail(String accountId, String emailId, String token, ApiCallback<Void> cb) {
        Request req = authRequest("accounts/" + accountId + "/emails/" + emailId, "DELETE", token, null);
        client.newCall(req).enqueue(voidCallback(cb));
    }

    // ── Attachments ──────────────────────────────────────────────────────────

    public void listAttachments(String accountId, String emailId, String token,
                                ApiCallback<List<AttachmentMeta>> cb) {
        Request req = authRequest(
                "accounts/" + accountId + "/emails/" + emailId + "/attachments", "GET", token, null);
        client.newCall(req).enqueue(new Callback() {
            @Override public void onFailure(Call call, IOException e) { cb.onFailure(e.getMessage()); }
            @Override public void onResponse(Call call, Response resp) throws IOException {
                try (ResponseBody body = resp.body()) {
                    if (!resp.isSuccessful() || body == null) { cb.onFailure("HTTP " + resp.code()); return; }
                    Type listType = new TypeToken<List<AttachmentMeta>>(){}.getType();
                    cb.onSuccess(gson.fromJson(body.string(), listType));
                }
            }
        });
    }

    public void downloadAttachment(String accountId, String emailId,
                                   String attachmentId, String token,
                                   ApiCallback<byte[]> cb) {
        Request req = authRequest(
                "accounts/" + accountId + "/emails/" + emailId + "/attachments/" + attachmentId,
                "GET", token, null);
        client.newCall(req).enqueue(new Callback() {
            @Override public void onFailure(Call call, IOException e) { cb.onFailure(e.getMessage()); }
            @Override public void onResponse(Call call, Response resp) throws IOException {
                try (ResponseBody body = resp.body()) {
                    if (!resp.isSuccessful() || body == null) { cb.onFailure("HTTP " + resp.code()); return; }
                    cb.onSuccess(body.bytes());
                }
            }
        });
    }

    // ── Device tokens ─────────────────────────────────────────────────────────

    /** Registers an FCM device token with the server. */
    public void registerDeviceToken(String accountId, String bearerToken,
                                    String fcmToken, ApiCallback<Void> cb) {
        String json = "{\"token\":\"" + fcmToken + "\",\"type\":\"fcm\"}";
        RequestBody body = RequestBody.create(json, MediaType.parse("application/json"));
        Request req = authRequest("accounts/" + accountId + "/device-token", "POST", bearerToken, body);
        client.newCall(req).enqueue(voidCallback(cb));
    }

    public void removeDeviceToken(String accountId, String bearerToken,
                                  String fcmToken, ApiCallback<Void> cb) {
        String json = "{\"token\":\"" + fcmToken + "\",\"type\":\"fcm\"}";
        RequestBody body = RequestBody.create(json, MediaType.parse("application/json"));
        Request req = authRequest("accounts/" + accountId + "/device-token", "DELETE", bearerToken, body);
        client.newCall(req).enqueue(voidCallback(cb));
    }

    // ── Helpers ───────────────────────────────────────────────────────────────

    private Request authRequest(String path, String method, String token, RequestBody body) {
        if (body == null && !method.equals("GET")) {
            body = RequestBody.create(new byte[0]);
        }
        Request.Builder b = new Request.Builder()
                .url(BASE_URL + "/" + path)
                .header("Authorization", "Bearer " + token);
        b.method(method, body);
        return b.build();
    }

    private Callback voidCallback(ApiCallback<Void> cb) {
        return new Callback() {
            @Override public void onFailure(Call call, IOException e) { cb.onFailure(e.getMessage()); }
            @Override public void onResponse(Call call, Response resp) {
                if (resp.isSuccessful()) cb.onSuccess(null);
                else cb.onFailure("HTTP " + resp.code());
                resp.close();
            }
        };
    }
}
