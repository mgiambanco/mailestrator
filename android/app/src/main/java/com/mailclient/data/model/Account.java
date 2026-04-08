package com.mailclient.data.model;

import java.util.Objects;

public class Account {
    public String id;
    public String address;
    // Local-only — never sent to the server; stored in EncryptedSharedPreferences.
    public String token = "";
    public boolean deviceTokenRegistered = false;
    // User-assigned nickname shown in the sidebar.
    public String label = "";

    /** Returns the label when set, otherwise the raw email address. */
    public String getDisplayName() {
        return (label != null && !label.isEmpty()) ? label : address;
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (!(o instanceof Account)) return false;
        return Objects.equals(id, ((Account) o).id);
    }

    @Override
    public int hashCode() {
        return Objects.hashCode(id);
    }
}
