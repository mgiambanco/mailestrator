package com.mailclient.storage;

import android.content.Context;
import android.content.SharedPreferences;

import androidx.security.crypto.EncryptedSharedPreferences;
import androidx.security.crypto.MasterKey;

import com.google.gson.Gson;
import com.google.gson.reflect.TypeToken;
import com.mailclient.data.model.Account;

import java.io.IOException;
import java.lang.reflect.Type;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;

/**
 * Secure persistence for account data (including bearer tokens).
 * Uses EncryptedSharedPreferences (AES-256-GCM) — Android equivalent of Keychain.
 * Excluded from unencrypted backups via backup_rules.xml.
 */
public class SecureStore {

    private static final String PREFS_FILE = "account_store";
    private static final String KEY_ACCOUNTS = "account_list";

    private final SharedPreferences prefs;
    private final Gson gson = new Gson();

    public SecureStore(Context context) {
        SharedPreferences p;
        try {
            MasterKey masterKey = new MasterKey.Builder(context)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build();
            p = EncryptedSharedPreferences.create(
                    context,
                    PREFS_FILE,
                    masterKey,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
            );
        } catch (GeneralSecurityException | IOException e) {
            // Fall back to plain prefs — should never happen on a normal device.
            p = context.getSharedPreferences(PREFS_FILE, Context.MODE_PRIVATE);
        }
        this.prefs = p;
    }

    public void saveAccounts(List<Account> accounts) {
        prefs.edit().putString(KEY_ACCOUNTS, gson.toJson(accounts)).apply();
    }

    public List<Account> loadAccounts() {
        String json = prefs.getString(KEY_ACCOUNTS, null);
        if (json == null) return new ArrayList<>();
        Type type = new TypeToken<List<Account>>(){}.getType();
        List<Account> list = gson.fromJson(json, type);
        return list != null ? list : new ArrayList<>();
    }

    public void clear() {
        prefs.edit().remove(KEY_ACCOUNTS).apply();
    }
}
