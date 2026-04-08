package com.mailclient.ui.detail;

import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.ImageView;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.core.content.FileProvider;
import androidx.recyclerview.widget.RecyclerView;

import com.mailclient.R;
import com.mailclient.data.model.AttachmentMeta;
import com.mailclient.network.ApiCallback;
import com.mailclient.network.ApiClient;

import java.io.File;
import java.io.FileOutputStream;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

public class AttachmentAdapter extends RecyclerView.Adapter<AttachmentAdapter.ViewHolder> {

    private final String accountId;
    private final String emailId;
    private final String token;
    private List<AttachmentMeta> items = new ArrayList<>();
    private final Set<String> downloading = new HashSet<>();

    public AttachmentAdapter(String accountId, String emailId, String token) {
        this.accountId = accountId;
        this.emailId   = emailId;
        this.token     = token;
    }

    public void setItems(List<AttachmentMeta> list) {
        items = list != null ? list : new ArrayList<>();
        notifyDataSetChanged();
    }

    @NonNull @Override
    public ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
        View v = LayoutInflater.from(parent.getContext())
                .inflate(R.layout.item_attachment, parent, false);
        return new ViewHolder(v);
    }

    @Override
    public void onBindViewHolder(@NonNull ViewHolder h, int pos) {
        AttachmentMeta att = items.get(pos);
        h.tvFilename.setText(att.filename != null ? att.filename : "attachment");
        h.tvSize.setText(formatSize(att.size));
        h.ivIcon.setImageResource(iconFor(att.content_type));
        boolean dl = downloading.contains(att.id);
        h.progressBar.setVisibility(dl ? View.VISIBLE : View.GONE);
        h.ivIcon.setVisibility(dl ? View.INVISIBLE : View.VISIBLE);
        h.itemView.setOnClickListener(v -> downloadAndOpen(h.itemView.getContext(), att));
    }

    @Override public int getItemCount() { return items.size(); }

    private void downloadAndOpen(Context ctx, AttachmentMeta att) {
        if (downloading.contains(att.id)) return;
        downloading.add(att.id);
        notifyDataSetChanged();

        ApiClient.getInstance().downloadAttachment(
                accountId, emailId, att.id, token,
                new ApiCallback<byte[]>() {
                    @Override public void onSuccess(byte[] data) {
                        downloading.remove(att.id);
                        saveAndOpen(ctx, att, data);
                        // Post back to notify
                        new android.os.Handler(android.os.Looper.getMainLooper())
                                .post(() -> notifyDataSetChanged());
                    }
                    @Override public void onFailure(String error) {
                        downloading.remove(att.id);
                        new android.os.Handler(android.os.Looper.getMainLooper()).post(() -> {
                            Toast.makeText(ctx, error, Toast.LENGTH_LONG).show();
                            notifyDataSetChanged();
                        });
                    }
                });
    }

    private void saveAndOpen(Context ctx, AttachmentMeta att, byte[] data) {
        try {
            File cacheDir = new File(ctx.getCacheDir(), "attachments");
            //noinspection ResultOfMethodCallIgnored
            cacheDir.mkdirs();
            String filename = att.filename != null ? att.filename : "attachment_" + att.id;
            File file = new File(cacheDir, filename);
            try (FileOutputStream fos = new FileOutputStream(file)) {
                fos.write(data);
            }
            Uri uri = FileProvider.getUriForFile(ctx,
                    ctx.getPackageName() + ".fileprovider", file);
            String mime = att.content_type != null ? att.content_type : "*/*";
            Intent intent = new Intent(Intent.ACTION_VIEW)
                    .setDataAndType(uri, mime)
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            new android.os.Handler(android.os.Looper.getMainLooper()).post(() -> {
                try {
                    ctx.startActivity(intent);
                } catch (Exception e) {
                    Toast.makeText(ctx, R.string.no_app_for_attachment, Toast.LENGTH_LONG).show();
                }
            });
        } catch (Exception e) {
            new android.os.Handler(android.os.Looper.getMainLooper()).post(() ->
                    Toast.makeText(ctx, R.string.download_failed, Toast.LENGTH_LONG).show());
        }
    }

    private static String formatSize(long bytes) {
        if (bytes < 1024) return bytes + " B";
        if (bytes < 1024 * 1024) return String.format("%.1f KB", bytes / 1024.0);
        return String.format("%.1f MB", bytes / (1024.0 * 1024));
    }

    private static int iconFor(String contentType) {
        if (contentType == null) return android.R.drawable.ic_menu_save;
        if (contentType.startsWith("image/")) return android.R.drawable.ic_menu_gallery;
        if (contentType.equals("application/pdf")) return android.R.drawable.ic_menu_agenda;
        return android.R.drawable.ic_menu_save;
    }

    static class ViewHolder extends RecyclerView.ViewHolder {
        ImageView ivIcon;
        TextView tvFilename, tvSize;
        ProgressBar progressBar;
        ViewHolder(View v) {
            super(v);
            ivIcon      = v.findViewById(R.id.iv_attachment_icon);
            tvFilename  = v.findViewById(R.id.tv_attachment_filename);
            tvSize      = v.findViewById(R.id.tv_attachment_size);
            progressBar = v.findViewById(R.id.progress_attachment);
        }
    }
}
