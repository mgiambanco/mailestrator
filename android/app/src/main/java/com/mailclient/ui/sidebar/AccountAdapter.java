package com.mailclient.ui.sidebar;

import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.recyclerview.widget.DiffUtil;
import androidx.recyclerview.widget.ListAdapter;
import androidx.recyclerview.widget.RecyclerView;

import com.mailclient.R;
import com.mailclient.data.model.Account;

import java.util.Map;

public class AccountAdapter extends ListAdapter<Account, AccountAdapter.ViewHolder> {

    public interface OnAccountClickListener {
        void onClick(Account account);
        void onLongClick(Account account, View anchor);
    }

    private OnAccountClickListener listener;
    private Map<String, Integer> unreadCounts;

    public AccountAdapter() {
        super(new DiffUtil.ItemCallback<Account>() {
            @Override public boolean areItemsTheSame(@NonNull Account a, @NonNull Account b) {
                return a.id.equals(b.id);
            }
            @Override public boolean areContentsTheSame(@NonNull Account a, @NonNull Account b) {
                return a.getDisplayName().equals(b.getDisplayName())
                        && a.address.equals(b.address);
            }
        });
    }

    public void setListener(OnAccountClickListener l) { this.listener = l; }
    public void setUnreadCounts(Map<String, Integer> counts) {
        this.unreadCounts = counts;
        notifyDataSetChanged();
    }

    @NonNull @Override
    public ViewHolder onCreateViewHolder(@NonNull ViewGroup parent, int viewType) {
        View v = LayoutInflater.from(parent.getContext())
                .inflate(R.layout.item_account, parent, false);
        return new ViewHolder(v);
    }

    @Override
    public void onBindViewHolder(@NonNull ViewHolder h, int pos) {
        Account account = getItem(pos);
        h.tvDisplayName.setText(account.getDisplayName());
        if (!account.label.isEmpty()) {
            h.tvAddress.setVisibility(View.VISIBLE);
            h.tvAddress.setText(account.address);
        } else {
            h.tvAddress.setVisibility(View.GONE);
        }
        int unread = unreadCounts != null ? unreadCounts.getOrDefault(account.id, 0) : 0;
        if (unread > 0) {
            h.tvBadge.setVisibility(View.VISIBLE);
            h.tvBadge.setText(String.valueOf(unread));
        } else {
            h.tvBadge.setVisibility(View.GONE);
        }
        h.itemView.setOnClickListener(v -> { if (listener != null) listener.onClick(account); });
        h.itemView.setOnLongClickListener(v -> {
            if (listener != null) listener.onLongClick(account, v);
            return true;
        });
    }

    static class ViewHolder extends RecyclerView.ViewHolder {
        TextView tvDisplayName, tvAddress, tvBadge;
        ViewHolder(View v) {
            super(v);
            tvDisplayName = v.findViewById(R.id.tv_display_name);
            tvAddress     = v.findViewById(R.id.tv_address);
            tvBadge       = v.findViewById(R.id.tv_badge);
        }
    }
}
