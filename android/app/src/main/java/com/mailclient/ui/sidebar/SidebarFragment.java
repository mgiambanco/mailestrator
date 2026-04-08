package com.mailclient.ui.sidebar;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.os.Bundle;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.EditText;
import android.widget.PopupMenu;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.appcompat.app.AlertDialog;
import androidx.fragment.app.Fragment;
import androidx.lifecycle.ViewModelProvider;
import androidx.recyclerview.widget.ItemTouchHelper;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.floatingactionbutton.FloatingActionButton;
import com.mailclient.R;
import com.mailclient.data.model.Account;
import com.mailclient.data.model.Email;
import com.mailclient.ui.viewmodel.MailViewModel;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Shown inside the navigation drawer. Lists all accounts with unread badges.
 * Mirrors SidebarView.swift + AccountRow.swift.
 */
public class SidebarFragment extends Fragment {

    private MailViewModel viewModel;
    private AccountAdapter adapter;

    @Nullable @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        return inflater.inflate(R.layout.fragment_sidebar, container, false);
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        viewModel = new ViewModelProvider(requireActivity()).get(MailViewModel.class);

        RecyclerView rv = view.findViewById(R.id.rv_accounts);
        rv.setLayoutManager(new LinearLayoutManager(requireContext()));
        adapter = new AccountAdapter();
        rv.setAdapter(adapter);

        adapter.setListener(new AccountAdapter.OnAccountClickListener() {
            @Override public void onClick(Account account) {
                viewModel.selectedAccount.setValue(account);
            }
            @Override public void onLongClick(Account account, View anchor) {
                showContextMenu(account, anchor);
            }
        });

        // Swipe to delete accounts
        new ItemTouchHelper(new ItemTouchHelper.SimpleCallback(0, ItemTouchHelper.LEFT) {
            @Override public boolean onMove(@NonNull RecyclerView rv,
                    @NonNull RecyclerView.ViewHolder vh, @NonNull RecyclerView.ViewHolder t) { return false; }
            @Override public void onSwiped(@NonNull RecyclerView.ViewHolder vh, int dir) {
                Account account = adapter.getCurrentList().get(vh.getAdapterPosition());
                viewModel.deleteAccount(account);
            }
        }).attachToRecyclerView(rv);

        FloatingActionButton fab = view.findViewById(R.id.fab_new_account);
        fab.setOnClickListener(v -> viewModel.createAccount());

        viewModel.accounts.observe(getViewLifecycleOwner(), accounts -> adapter.submitList(accounts));
        viewModel.emailsByAccount.observe(getViewLifecycleOwner(), map -> {
            Map<String, Integer> counts = new HashMap<>();
            for (Map.Entry<String, List<Email>> e : map.entrySet()) {
                int unread = 0;
                for (Email email : e.getValue()) { if (!email.read) unread++; }
                counts.put(e.getKey(), unread);
            }
            adapter.setUnreadCounts(counts);
        });
    }

    // ── Context menu ──────────────────────────────────────────────────────────

    private void showContextMenu(Account account, View anchor) {
        PopupMenu menu = new PopupMenu(requireContext(), anchor);
        menu.getMenu().add(0, 1, 0, R.string.action_copy_address);
        menu.getMenu().add(0, 2, 1, R.string.action_rename);
        if (account.label != null && !account.label.isEmpty()) {
            menu.getMenu().add(0, 3, 2, R.string.action_clear_label);
        }
        menu.setOnMenuItemClickListener(item -> {
            switch (item.getItemId()) {
                case 1: copyAddress(account); return true;
                case 2: showRenameDialog(account); return true;
                case 3: viewModel.renameAccount(account, ""); return true;
            }
            return false;
        });
        menu.show();
    }

    private void copyAddress(Account account) {
        ClipboardManager cm = (ClipboardManager) requireContext()
                .getSystemService(Context.CLIPBOARD_SERVICE);
        cm.setPrimaryClip(ClipData.newPlainText("email address", account.address));
        Toast.makeText(requireContext(), R.string.copied, Toast.LENGTH_SHORT).show();
    }

    private void showRenameDialog(Account account) {
        EditText input = new EditText(requireContext());
        input.setText(account.label);
        input.setHint(R.string.rename_hint);
        new AlertDialog.Builder(requireContext())
                .setTitle(R.string.rename_account)
                .setView(input)
                .setPositiveButton(R.string.save, (d, w) -> {
                    String label = input.getText().toString().trim();
                    viewModel.renameAccount(account, label);
                })
                .setNegativeButton(android.R.string.cancel, null)
                .show();
    }
}
