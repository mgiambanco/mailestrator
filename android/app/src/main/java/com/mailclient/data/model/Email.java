package com.mailclient.data.model;

import com.google.gson.annotations.SerializedName;

import java.util.List;
import java.util.Objects;

public class Email {
    public String id;
    public String account_id;

    // "from" is a Java keyword — map the JSON key to a valid field name.
    @SerializedName("from")
    public String fromAddr;

    public String subject;
    public String body_text;
    public String body_html;
    public String received_at; // ISO-8601 string
    public boolean read;
    public int attachment_count;
    public List<AttachmentMeta> attachments;

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (!(o instanceof Email)) return false;
        return Objects.equals(id, ((Email) o).id);
    }

    @Override
    public int hashCode() {
        return Objects.hashCode(id);
    }
}
