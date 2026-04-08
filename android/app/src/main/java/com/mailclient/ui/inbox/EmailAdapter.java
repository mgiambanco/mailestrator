package com.mailclient.ui.inbox;

import android.text.format.DateUtils;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.ImageView;
import android.widget.ProgressBar;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.recyclerview.widget.DiffUtil;
import androidx.recyclerview.widget.ListAdapter;
import androidx.recyclerview.widget.RecyclerView;

import com.mailclient.R;
import com.mailclient.data.model.Email;

import java.text.ParseException;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.TimeZone;

public class EmailAdapter extends ListAdapter<Email, RecyclerView.ViewHolder> {

    private static final int TYPE_EMAIL     = 0;
    private static final int TYPE_LOAD_MORE = 1;
    private static final int TYPE_FOOTER    = 2; // sentinel item marker

    public interface EmailClickListener    { void onClick(Email email); }
    public interface LoadMoreClickListener { void onLoadMore(); }

    private EmailClickListener    emailListener;
    private LoadMoreClickListener loadMoreListener;
    private boolean showLoadMore   = false;
    private boolean loadingMore    = false;

    public EmailAdapter() {
        super(new DiffUtil.ItemCallback<Email>() {
            @Override public boolean areItemsTheSame(@NonNull Email a, @NonNull Email b) {
                return a.id.equals(b.id);
            }
            @Override public boolean areContentsTheSame(@NonNull Email a, @NonNull Email b) {
                return a.read == b.read && a.subject.equals(b.subject);
            }
        });
    }

    public void setEmailClickListener(EmailClickListener l)       { emailListener     = l; }
    public void setLoadMoreClickListener(LoadMoreClickListener l) { loadMoreListener  = l; }

    public void setShowLoadMore(boolean show, boolean loading) {
        boolean changed = showLoadMore != show || loadingMore != loading;
        showLoadMore = show;
        loadingMore  = loading;
        if (changed) notifyItemChanged(getItemCount() - 1);
    }

    @Override public int getItemCount() {
        return super.getItemCount() + (showLoadMore ? 1 : 0);
    }

    @Override public int getItemViewType(int position) {
        return (showLoadMore && position == super.getItemCount()) ? TYPE_LOAD_MORE : TYPE_EMAIL;
    }

    @NonNull @Override
    public RecyclerView.ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
        LayoutInflater inf = LayoutInflater.from(parent.getContext());
        if (viewType == TYPE_LOAD_MORE) {
            return new LoadMoreViewHolder(inf.inflate(R.layout.item_load_more, parent, false));
        }
        return new EmailViewHolder(inf.inflate(R.layout.item_email, parent, false));
    }

    @Override
    public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int pos) {
        if (holder instanceof LoadMoreViewHolder) {
            LoadMoreViewHolder lm = (LoadMoreViewHolder) holder;
            lm.progressBar.setVisibility(loadingMore ? View.VISIBLE : View.GONE);
            lm.btnLoadMore.setVisibility(loadingMore ? View.GONE : View.VISIBLE);
            lm.btnLoadMore.setOnClickListener(v -> { if (loadMoreListener != null) loadMoreListener.onLoadMore(); });
            return;
        }
        Email email = getItem(pos);
        EmailViewHolder h = (EmailViewHolder) holder;
        h.tvSender.setText(email.fromAddr != null ? email.fromAddr : "");
        h.tvSender.setTypeface(null, email.read
                ? android.graphics.Typeface.NORMAL : android.graphics.Typeface.BOLD);
        h.tvSubject.setText(email.subject != null && !email.subject.isEmpty()
                ? email.subject : h.itemView.getContext().getString(R.string.no_subject));
        h.tvPreview.setText(email.body_text != null ? email.body_text : "");
        h.tvTimestamp.setText(parseRelative(email.received_at));
        h.viewUnread.setVisibility(email.read ? View.INVISIBLE : View.VISIBLE);
        h.ivAttachment.setVisibility(email.attachment_count > 0 ? View.VISIBLE : View.GONE);
        h.itemView.setOnClickListener(v -> { if (emailListener != null) emailListener.onClick(email); });
    }

    // ── Date parsing ──────────────────────────────────────────────────────────

    private String parseRelative(String iso) {
        if (iso == null) return "";
        try {
            SimpleDateFormat sdf = new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.US);
            sdf.setTimeZone(TimeZone.getTimeZone("UTC"));
            Date date = sdf.parse(iso);
            if (date == null) return iso;
            return DateUtils.getRelativeTimeSpanString(
                    date.getTime(), System.currentTimeMillis(),
                    DateUtils.MINUTE_IN_MILLIS).toString();
        } catch (ParseException e) {
            return iso;
        }
    }

    // ── ViewHolders ───────────────────────────────────────────────────────────

    static class EmailViewHolder extends RecyclerView.ViewHolder {
        View     viewUnread;
        TextView tvSender, tvSubject, tvPreview, tvTimestamp;
        ImageView ivAttachment;
        EmailViewHolder(View v) {
            super(v);
            viewUnread   = v.findViewById(R.id.view_unread);
            tvSender     = v.findViewById(R.id.tv_sender);
            tvSubject    = v.findViewById(R.id.tv_subject);
            tvPreview    = v.findViewById(R.id.tv_preview);
            tvTimestamp  = v.findViewById(R.id.tv_timestamp);
            ivAttachment = v.findViewById(R.id.iv_attachment);
        }
    }

    static class LoadMoreViewHolder extends RecyclerView.ViewHolder {
        ProgressBar progressBar;
        Button btnLoadMore;
        LoadMoreViewHolder(View v) {
            super(v);
            progressBar = v.findViewById(R.id.progress_load_more);
            btnLoadMore = v.findViewById(R.id.btn_load_more);
        }
    }
}
