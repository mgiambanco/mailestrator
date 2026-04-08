package com.mailclient.ui.viewmodel;

import android.app.Application;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.Context;
import android.os.Build;

import androidx.annotation.NonNull;
import androidx.core.app.NotificationManagerCompat;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.MutableLiveData;

import com.mailclient.data.model.Account;
import com.mailclient.data.model.Email;
import com.mailclient.data.model.EmailPage;
import com.mailclient.network.ApiCallback;
import com.mailclient.network.ApiClient;
import com.mailclient.network.WebSocketManager;
import com.mailclient.storage.SecureStore;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Central state container. Mirrors MailStore.swift.
 * Scoped to the Activity so it survives rotation.
 */
public class MailViewModel extends AndroidViewModel implements WebSocketManager.NewEmailListener {

    // ── State (LiveData) ──────────────────────────────────────────────────────

    public final MutableLiveData<List<Account>> accounts = new MutableLiveData<>(new ArrayList<>());
    public final MutableLiveData<Account> selectedAccount = new MutableLiveData<>(null);
    public final MutableLiveData<Email> selectedEmail = new MutableLiveData<>(null);
    public final MutableLiveData<Map<String, List<Email>>> emailsByAccount = new MutableLiveData<>(new HashMap<>());
    public final MutableLiveData<Map<String, Boolean>> hasMoreByAccount = new MutableLiveData<>(new HashMap<>());
    public final MutableLiveData<Map<String, Boolean>> loadingMoreByAccount = new MutableLiveData<>(new HashMap<>());
    public final MutableLiveData<String> errorMessage = new MutableLiveData<>(null);

    // ── Infrastructure ────────────────────────────────────────────────────────

    private final SecureStore secureStore;
    private final ApiClient api = ApiClient.getInstance();
    private final WebSocketManager wsManager;

    // Cursor for pagination per account (received_at of oldest loaded email).
    private final Map<String, String> nextCursorByAccount = new HashMap<>();
    // Pending FCM token to register once accounts are known.
    private String pendingFcmToken = null;

    public MailViewModel(@NonNull Application app) {
        super(app);
        secureStore = new SecureStore(app);
        wsManager = new WebSocketManager(api.getHttpClient());
        wsManager.setNewEmailListener(this);
        createNotificationChannel();
        loadPersistedAccounts();
    }

    // ── Account management ────────────────────────────────────────────────────

    public void createAccount() {
        api.createAccount(new ApiCallback<Account>() {
            @Override public void onSuccess(Account account) {
                // Register FCM token if we have one waiting.
                if (pendingFcmToken != null) {
                    api.registerDeviceToken(account.id, account.token, pendingFcmToken,
                            new ApiCallback<Void>() {
                                @Override public void onSuccess(Void v) {
                                    account.deviceTokenRegistered = true;
                                    persist();
                                }
                                @Override public void onFailure(String e) {}
                            });
                }
                List<Account> list = new ArrayList<>(currentAccounts());
                list.add(account);
                accounts.postValue(list);
                Map<String, List<Email>> map = new HashMap<>(currentEmailMap());
                map.put(account.id, new ArrayList<>());
                emailsByAccount.postValue(map);
                persist();
                wsManager.connect(account.id, account.token);
                if (selectedAccount.getValue() == null) {
                    selectedAccount.postValue(account);
                }
            }
            @Override public void onFailure(String e) { errorMessage.postValue(e); }
        });
    }

    public void deleteAccount(Account account) {
        wsManager.disconnect(account.id);
        api.deleteAccount(account.id, account.token, new ApiCallback<Void>() {
            @Override public void onSuccess(Void v) {}
            @Override public void onFailure(String e) {}
        });
        List<Account> list = new ArrayList<>(currentAccounts());
        list.remove(account);
        accounts.postValue(list);
        Map<String, List<Email>> map = new HashMap<>(currentEmailMap());
        map.remove(account.id);
        emailsByAccount.postValue(map);
        persist();
        if (account.equals(selectedAccount.getValue())) {
            selectedAccount.postValue(list.isEmpty() ? null : list.get(0));
        }
        updateBadge();
    }

    public void renameAccount(Account account, String label) {
        List<Account> list = currentAccounts();
        for (Account a : list) {
            if (a.id.equals(account.id)) {
                a.label = label;
                break;
            }
        }
        accounts.postValue(new ArrayList<>(list));
        persist();
        // Refresh selected so the toolbar title updates.
        if (account.equals(selectedAccount.getValue())) {
            for (Account a : list) {
                if (a.id.equals(account.id)) { selectedAccount.postValue(a); break; }
            }
        }
    }

    // ── Email loading ─────────────────────────────────────────────────────────

    public void loadEmails(String accountId) {
        String token = tokenFor(accountId);
        api.listEmails(accountId, token, 50, null, new ApiCallback<EmailPage>() {
            @Override public void onSuccess(EmailPage page) {
                Map<String, List<Email>> map = new HashMap<>(currentEmailMap());
                map.put(accountId, page.emails);
                emailsByAccount.postValue(map);
                Map<String, Boolean> hm = new HashMap<>(currentHasMore());
                hm.put(accountId, page.has_more);
                hasMoreByAccount.postValue(hm);
                nextCursorByAccount.put(accountId,
                        page.emails.isEmpty() ? null : page.emails.get(page.emails.size() - 1).received_at);
                updateBadge();
            }
            @Override public void onFailure(String e) { errorMessage.postValue(e); }
        });
    }

    public void loadMoreEmails(String accountId) {
        Boolean isLoading = currentLoadingMore().get(accountId);
        Boolean hasMore = currentHasMore().get(accountId);
        if (Boolean.TRUE.equals(isLoading) || !Boolean.TRUE.equals(hasMore)) return;
        String cursor = nextCursorByAccount.get(accountId);
        if (cursor == null) return;

        Map<String, Boolean> lm = new HashMap<>(currentLoadingMore());
        lm.put(accountId, true);
        loadingMoreByAccount.postValue(lm);

        String token = tokenFor(accountId);
        api.listEmails(accountId, token, 50, cursor, new ApiCallback<EmailPage>() {
            @Override public void onSuccess(EmailPage page) {
                Map<String, List<Email>> map = new HashMap<>(currentEmailMap());
                List<Email> existing = new ArrayList<>(map.getOrDefault(accountId, new ArrayList<>()));
                existing.addAll(page.emails);
                map.put(accountId, existing);
                emailsByAccount.postValue(map);

                Map<String, Boolean> hm = new HashMap<>(currentHasMore());
                hm.put(accountId, page.has_more);
                hasMoreByAccount.postValue(hm);

                if (!page.emails.isEmpty()) {
                    nextCursorByAccount.put(accountId,
                            page.emails.get(page.emails.size() - 1).received_at);
                }

                Map<String, Boolean> l = new HashMap<>(currentLoadingMore());
                l.put(accountId, false);
                loadingMoreByAccount.postValue(l);
                updateBadge();
            }
            @Override public void onFailure(String e) {
                Map<String, Boolean> l = new HashMap<>(currentLoadingMore());
                l.put(accountId, false);
                loadingMoreByAccount.postValue(l);
                errorMessage.postValue(e);
            }
        });
    }

    public void deleteEmail(Email email) {
        String token = tokenFor(email.account_id);
        api.deleteEmail(email.account_id, email.id, token, new ApiCallback<Void>() {
            @Override public void onSuccess(Void v) {}
            @Override public void onFailure(String e) {}
        });
        Map<String, List<Email>> map = new HashMap<>(currentEmailMap());
        List<Email> list = new ArrayList<>(map.getOrDefault(email.account_id, new ArrayList<>()));
        list.remove(email);
        map.put(email.account_id, list);
        emailsByAccount.postValue(map);
        if (email.equals(selectedEmail.getValue())) selectedEmail.postValue(null);
        updateBadge();
    }

    // ── WebSocketManager.NewEmailListener ────────────────────────────────────

    @Override
    public void onNewEmail(String accountId, Email email) {
        Map<String, List<Email>> map = new HashMap<>(currentEmailMap());
        List<Email> list = new ArrayList<>(map.getOrDefault(accountId, new ArrayList<>()));
        list.add(0, email);
        map.put(accountId, list);
        emailsByAccount.postValue(map);
        updateBadge();
    }

    // ── FCM token ─────────────────────────────────────────────────────────────

    public void setFcmToken(String fcmToken) {
        pendingFcmToken = fcmToken;
        for (Account a : currentAccounts()) {
            if (!a.deviceTokenRegistered) {
                api.registerDeviceToken(a.id, a.token, fcmToken, new ApiCallback<Void>() {
                    @Override public void onSuccess(Void v) {
                        a.deviceTokenRegistered = true;
                        persist();
                    }
                    @Override public void onFailure(String e) {}
                });
            }
        }
    }

    // ── WebSocket lifecycle ───────────────────────────────────────────────────

    public void connectAllWebSockets() {
        for (Account a : currentAccounts()) wsManager.connect(a.id, a.token);
    }

    // ── Badge ─────────────────────────────────────────────────────────────────

    public int getTotalUnreadCount() {
        int total = 0;
        for (List<Email> list : currentEmailMap().values()) {
            for (Email e : list) { if (!e.read) total++; }
        }
        return total;
    }

    public void updateBadge() {
        int count = getTotalUnreadCount();
        // Post a silent summary notification carrying the badge count.
        // On supported launchers this updates the app icon badge.
        NotificationManagerCompat nm = NotificationManagerCompat.from(getApplication());
        // Cancel and re-post the summary notification to refresh the badge.
        nm.cancel(0);
        if (count > 0) {
            androidx.core.app.NotificationCompat.Builder nb =
                    new androidx.core.app.NotificationCompat.Builder(getApplication(), "new_email")
                            .setSmallIcon(android.R.drawable.ic_dialog_email)
                            .setContentTitle(count + " unread message" + (count == 1 ? "" : "s"))
                            .setSilent(true)
                            .setOngoing(false)
                            .setNumber(count)
                            .setGroup("mail_group")
                            .setGroupSummary(true);
            try { nm.notify(0, nb.build()); } catch (SecurityException ignored) {}
        }
    }

    // ── Persistence ───────────────────────────────────────────────────────────

    private void loadPersistedAccounts() {
        List<Account> saved = secureStore.loadAccounts();
        accounts.setValue(saved);
        Map<String, List<Email>> map = new HashMap<>();
        for (Account a : saved) map.put(a.id, new ArrayList<>());
        emailsByAccount.setValue(map);
        if (!saved.isEmpty()) selectedAccount.setValue(saved.get(0));
    }

    private void persist() {
        secureStore.saveAccounts(currentAccounts());
    }

    // ── Helpers ───────────────────────────────────────────────────────────────

    private String tokenFor(String accountId) {
        for (Account a : currentAccounts()) {
            if (a.id.equals(accountId)) return a.token;
        }
        return "";
    }

    private List<Account> currentAccounts() {
        List<Account> v = accounts.getValue();
        return v != null ? v : new ArrayList<>();
    }

    private Map<String, List<Email>> currentEmailMap() {
        Map<String, List<Email>> v = emailsByAccount.getValue();
        return v != null ? v : new HashMap<>();
    }

    private Map<String, Boolean> currentHasMore() {
        Map<String, Boolean> v = hasMoreByAccount.getValue();
        return v != null ? v : new HashMap<>();
    }

    private Map<String, Boolean> currentLoadingMore() {
        Map<String, Boolean> v = loadingMoreByAccount.getValue();
        return v != null ? v : new HashMap<>();
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                    "new_email", "New Messages", NotificationManager.IMPORTANCE_HIGH);
            ch.setDescription("Notifications for new emails");
            NotificationManager nm = getApplication().getSystemService(NotificationManager.class);
            if (nm != null) nm.createNotificationChannel(ch);
        }
    }

    @Override
    protected void onCleared() {
        super.onCleared();
        wsManager.disconnectAll();
    }
}
